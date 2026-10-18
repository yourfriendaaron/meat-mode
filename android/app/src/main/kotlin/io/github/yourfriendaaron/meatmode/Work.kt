package io.github.yourfriendaaron.meatmode

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * Background calls to the hub with the device token, through WorkManager so a
 * flaky network retries them. Secrets never go into work input data, which
 * WorkManager stores in the clear: workers read them from [Secrets].
 */
object Work {
    private const val CONFIRM = "confirm"
    private const val RENEW = "renew"
    private const val REVOKE = "revoke"

    fun confirm(context: Context, device: String) =
        enqueue(context, CONFIRM, OneTimeWorkRequestBuilder<ConfirmWorker>().setInputData(workDataOf(ConfirmWorker.DEVICE to device)))

    fun renew(context: Context) = enqueue(context, RENEW, OneTimeWorkRequestBuilder<RenewWorker>())

    fun cancelRenew(context: Context) {
        WorkManager.getInstance(context).cancelUniqueWork(RENEW)
    }

    fun revoke(context: Context, origin: String) =
        enqueue(context, REVOKE, OneTimeWorkRequestBuilder<RevokeWorker>().setInputData(workDataOf(RevokeWorker.ORIGIN to origin)))

    fun action(context: Context, tag: String, button: String, key: String, body: String) = enqueue(
        context,
        "action-$tag-$button",
        OneTimeWorkRequestBuilder<ActionWorker>().setInputData(
            workDataOf(ActionWorker.TAG to tag, ActionWorker.BUTTON to button, ActionWorker.KEY to key, ActionWorker.BODY to body),
        ),
    )

    private fun enqueue(context: Context, name: String, builder: androidx.work.OneTimeWorkRequest.Builder) {
        val request = builder
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 10, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(context).enqueueUniqueWork(name, ExistingWorkPolicy.REPLACE, request)
    }

    /** Tries at most this many times before giving up. */
    const val MAX_ATTEMPTS = 5
}

/** Swaps the confirmation code for the device token: `POST /api/devices/{id}/confirm`. */
class ConfirmWorker(context: Context, params: WorkerParameters) : Worker(context, params) {
    override fun doWork(): Result {
        val ctx = applicationContext
        val device = inputData.getString(DEVICE) ?: return Result.failure()
        val origin = Store(ctx).origin ?: return Result.failure()
        val secrets = Secrets(ctx)
        val code = secrets.get(Secrets.CONFIRM_CODE) ?: return Result.failure()
        val res = try {
            HubClient(origin).send("POST", "/api/devices/$device/confirm", JSONObject().put("code", code).toString(), null)
        } catch (e: IOException) {
            PushLog.add(ctx, "confirm: ${e.javaClass.simpleName}")
            return retryOrFail()
        }
        PushLog.add(ctx, "confirm: HTTP ${res.code}")
        return when {
            res.code == 200 -> {
                val token = try {
                    JSONObject(res.body).optString("token")
                } catch (_: JSONException) {
                    ""
                }
                secrets.set(Secrets.CONFIRM_CODE, null)
                if (!token.startsWith("mmd_")) return Result.failure()
                secrets.set(Secrets.TOKEN, token)
                Store(ctx).deviceId = device
                Result.success()
            }
            res.code >= 500 -> retryOrFail()
            else -> {
                secrets.set(Secrets.CONFIRM_CODE, null)
                Result.failure()
            }
        }
    }

    private fun retryOrFail(): Result = if (runAttemptCount + 1 < Work.MAX_ATTEMPTS) Result.retry() else Result.failure()

    companion object {
        const val DEVICE = "device"
    }
}

/** Renews the device and hands the hub its current endpoint: `PUT /api/devices/self`. */
class RenewWorker(context: Context, params: WorkerParameters) : Worker(context, params) {
    override fun doWork(): Result {
        val ctx = applicationContext
        val store = Store(ctx)
        val origin = store.origin ?: return Result.failure()
        val token = Secrets(ctx).get(Secrets.TOKEN) ?: return Result.success()
        val endpoint = store.endpoint ?: return Result.success()
        val body = JSONObject()
            .put("endpoint", endpoint)
            .put("keys", JSONObject().put("p256dh", store.p256dh).put("auth", store.auth))
        val res = try {
            HubClient(origin).send("PUT", "/api/devices/self", body.toString(), token)
        } catch (e: IOException) {
            PushLog.add(ctx, "renew: ${e.javaClass.simpleName}")
            return if (runAttemptCount + 1 < Work.MAX_ATTEMPTS) Result.retry() else Result.failure()
        }
        PushLog.add(ctx, "renew: HTTP ${res.code}")
        return when {
            res.code in 200..299 -> Result.success()
            // The hub revoked this device: forget the token, and reminders read as off.
            res.code == 401 -> {
                Registration.forget(ctx, revoke = false)
                Result.failure()
            }
            res.code >= 500 -> if (runAttemptCount + 1 < Work.MAX_ATTEMPTS) Result.retry() else Result.failure()
            else -> Result.failure()
        }
    }
}

/** Tells the hub this device is gone: `DELETE /api/devices/self`, with the token kept for it. */
class RevokeWorker(context: Context, params: WorkerParameters) : Worker(context, params) {
    override fun doWork(): Result {
        val ctx = applicationContext
        val secrets = Secrets(ctx)
        val origin = inputData.getString(ORIGIN) ?: return Result.failure()
        val token = secrets.get(Secrets.REVOKE_TOKEN) ?: return Result.success()
        val res = try {
            HubClient(origin).send("DELETE", "/api/devices/self", null, token)
        } catch (e: IOException) {
            PushLog.add(ctx, "revoke: ${e.javaClass.simpleName}")
            if (runAttemptCount + 1 < Work.MAX_ATTEMPTS) return Result.retry()
            secrets.set(Secrets.REVOKE_TOKEN, null)
            return Result.failure()
        }
        PushLog.add(ctx, "revoke: HTTP ${res.code}")
        if (res.code >= 500 && runAttemptCount + 1 < Work.MAX_ATTEMPTS) return Result.retry()
        secrets.set(Secrets.REVOKE_TOKEN, null)
        return Result.success()
    }

    companion object {
        const val ORIGIN = "origin"
    }
}

/**
 * Posts a notification button's action with the device token. When the hub
 * refuses it, or it cannot be sent, the notification comes back as one that
 * opens the item, so its card says why.
 */
class ActionWorker(context: Context, params: WorkerParameters) : Worker(context, params) {
    override fun doWork(): Result {
        val ctx = applicationContext
        val tag = inputData.getString(TAG) ?: return Result.failure()
        val button = inputData.getString(BUTTON).orEmpty()
        val key = inputData.getString(KEY) ?: return Result.failure()
        val body = inputData.getString(BODY) ?: return Result.failure()
        val origin = Store(ctx).origin
        val token = Secrets(ctx).get(Secrets.TOKEN)
        if (origin == null || token == null) {
            return refused(ctx, tag, key, ctx.getString(R.string.action_refused_off))
        }
        val res = try {
            HubClient(origin).send("POST", "/api/actions", body, token)
        } catch (e: IOException) {
            PushLog.add(ctx, "action $button: ${e.javaClass.simpleName}")
            return if (runAttemptCount + 1 < Work.MAX_ATTEMPTS) {
                Result.retry()
            } else {
                refused(ctx, tag, key, ctx.getString(R.string.action_refused_offline))
            }
        }
        PushLog.add(ctx, "action $button: HTTP ${res.code}")
        return when {
            res.code in 200..299 -> Result.success()
            res.code >= 500 && runAttemptCount + 1 < Work.MAX_ATTEMPTS -> Result.retry()
            else -> {
                if (res.code == 401) Registration.forget(ctx, revoke = false)
                val reason = try {
                    JSONObject(res.body).optString("error")
                } catch (_: JSONException) {
                    ""
                }
                refused(ctx, tag, key, reason.ifBlank { ctx.getString(R.string.action_refused_hub) })
            }
        }
    }

    private fun refused(ctx: Context, tag: String, key: String, message: String): Result {
        Notifier.showRefused(ctx, tag, key, message)
        return Result.failure()
    }

    companion object {
        const val TAG = "tag"
        const val BUTTON = "button"
        const val KEY = "key"
        const val BODY = "body"
    }
}
