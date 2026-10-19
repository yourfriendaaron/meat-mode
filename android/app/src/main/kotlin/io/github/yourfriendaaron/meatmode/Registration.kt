package io.github.yourfriendaaron.meatmode

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import androidx.core.app.NotificationManagerCompat
import org.json.JSONObject
import org.unifiedpush.android.connector.FailedReason
import org.unifiedpush.android.connector.UnifiedPush
import java.time.Instant

/**
 * The app's push registration, shared by the activity, the push service and
 * the workers: what `push-status` reports, what happens on each UnifiedPush
 * event, and forgetting the device. docs/api.md, Devices and Android app.
 */
object Registration {
    private val main = Handler(Looper.getMainLooper())
    @Volatile
    private var pending: ((Result<JSONObject>) -> Unit)? = null
    private val timeout = Runnable { finish(Result.failure(Failure("The push service did not answer. Try again."))) }

    /** A failure whose message the page shows as it is. */
    class Failure(message: String) : Exception(message)

    /** Waits for the endpoint of the registration push-on just asked for. Main thread. */
    fun await(done: (Result<JSONObject>) -> Unit) {
        pending?.invoke(Result.failure(Failure("Another request to turn reminders on replaced this one.")))
        pending = done
        main.removeCallbacks(timeout)
        main.postDelayed(timeout, 30_000)
    }

    fun finish(result: Result<JSONObject>) {
        main.post {
            main.removeCallbacks(timeout)
            val p = pending
            pending = null
            p?.invoke(result)
        }
    }

    fun status(context: Context): JSONObject {
        val store = Store(context)
        val hasToken = Secrets(context).get(Secrets.TOKEN) != null
        val state = PushState.of(
            hasDistributor = UnifiedPush.getDistributors(context).isNotEmpty(),
            endpoint = store.endpoint,
            hasToken = hasToken,
            notificationsEnabled = NotificationManagerCompat.from(context).areNotificationsEnabled(),
            registeredAt = store.registeredAt,
            now = System.currentTimeMillis(),
        )
        val out = JSONObject().put("state", state)
        if (state == PushState.ON || state == PushState.WAITING) out.put("endpoint", store.endpoint)
        distributorName(context)?.let { out.put("distributor", it) }
        out.put("lastPush", if (store.lastPush > 0) Instant.ofEpochMilli(store.lastPush).toString() else JSONObject.NULL)
        out.put("batteryExempt", batteryExempt(context))
        return out
    }

    /** The saved distributor's name, as the user knows it. */
    fun distributorName(context: Context): String? {
        val pkg = UnifiedPush.getAckDistributor(context) ?: return null
        if (pkg == context.packageName) return context.getString(R.string.distributor_embedded)
        return try {
            val pm = context.packageManager
            pm.getApplicationLabel(pm.getApplicationInfo(pkg, 0)).toString()
        } catch (_: PackageManager.NameNotFoundException) {
            pkg
        }
    }

    /** Whether the distributor is another app, which needs Meat Mode unrestricted to wake it. */
    fun usesDistributorApp(context: Context): Boolean =
        UnifiedPush.getAckDistributor(context).let { it != null && it != context.packageName }

    fun batteryExempt(context: Context): Boolean =
        context.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(context.packageName)

    /** The `push-on` answer for the endpoint the distributor gave. */
    fun subscription(store: Store): JSONObject? {
        val endpoint = store.endpoint ?: return null
        val keys = JSONObject().put("p256dh", store.p256dh).put("auth", store.auth)
        return JSONObject()
            .put("subscription", JSONObject().put("endpoint", endpoint).put("keys", keys))
            .put("name", Build.MODEL.orEmpty().trim().take(64))
    }

    fun onNewEndpoint(context: Context, url: String, p256dh: String?, auth: String?) {
        if (p256dh == null || auth == null) {
            PushLog.add(context, "new endpoint without keys from ${PushLog.host(url)}: ignored")
            finish(Result.failure(Failure("The push service gave no encryption keys, so the hub cannot reach this phone.")))
            return
        }
        val store = Store(context)
        store.setEndpoint(url, p256dh, auth)
        PushLog.add(context, "new endpoint at ${PushLog.host(url)}")
        if (pending != null) {
            finish(Result.success(subscription(store)!!))
        }
        if (Secrets(context).get(Secrets.TOKEN) != null) Work.renew(context)
    }

    fun onConfirm(context: Context, device: String, code: String) {
        // A code that arrives after push-off belongs to no registration; taking
        // it would show reminders on with no endpoint.
        if (Store(context).endpoint == null) {
            PushLog.add(context, "confirmation code with no registration: ignored")
            return
        }
        Secrets(context).set(Secrets.CONFIRM_CODE, code)
        Work.confirm(context, device)
    }

    fun onRegistrationFailed(context: Context, reason: FailedReason) {
        PushLog.add(context, "registration failed: $reason")
        val message = when (reason) {
            FailedReason.VAPID_REQUIRED -> "The push service needs the hub's VAPID key and did not accept it."
            FailedReason.NETWORK -> "The push service could not be reached. Check the connection and try again."
            FailedReason.ACTION_REQUIRED -> "The push service app needs attention: open it and finish its setup."
            FailedReason.INTERNAL_ERROR -> "The push service could not register Meat Mode."
        }
        finish(Result.failure(Failure(message)))
    }

    fun onUnregistered(context: Context) {
        PushLog.add(context, "unregistered by the distributor")
        forget(context, revoke = true, unregister = false)
        finish(Result.failure(Failure("The push service ended the registration.")))
    }

    /** Registers again with the saved distributor, as UnifiedPush recommends on each start. */
    fun refresh(context: Context) {
        val vapid = Store(context).vapidKey ?: return
        if (Secrets(context).get(Secrets.TOKEN) == null) return
        Work.renew(context)
        if (UnifiedPush.getAckDistributor(context) == null) return
        try {
            UnifiedPush.register(context, messageForDistributor = context.getString(R.string.app_name), vapid = vapid)
        } catch (e: Exception) {
            PushLog.add(context, "register again failed: ${e.javaClass.simpleName}")
        }
    }

    /**
     * Forgets the device: token, code, endpoint and keys. With [revoke], the hub
     * is told too (`DELETE /api/devices/self`), in the background, from a copy
     * of the token kept only until that call is made.
     */
    fun forget(context: Context, revoke: Boolean, unregister: Boolean = true) {
        val secrets = Secrets(context)
        val store = Store(context)
        val token = secrets.get(Secrets.TOKEN)
        val origin = store.origin
        if (revoke && token != null && origin != null) {
            secrets.set(Secrets.REVOKE_TOKEN, token)
            Work.revoke(context, origin)
        }
        secrets.set(Secrets.TOKEN, null)
        secrets.set(Secrets.CONFIRM_CODE, null)
        store.clearPush()
        Work.cancelRenew(context)
        if (unregister) {
            try {
                UnifiedPush.unregister(context)
            } catch (e: Exception) {
                PushLog.add(context, "unregister failed: ${e.javaClass.simpleName}")
            }
        }
        PushLog.add(context, if (revoke) "device forgotten and revoked" else "device forgotten")
    }
}
