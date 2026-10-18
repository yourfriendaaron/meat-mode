package io.github.yourfriendaaron.meatmode

import org.json.JSONException
import org.json.JSONObject

/** What the bridge needs from the app's push side; MainActivity provides it. */
interface PushControl {
    /** The `push-status` answer, without its id. */
    fun status(): JSONObject

    /** Turns reminders on; [done] gets the `push-on` answer or a plain error message. */
    fun pushOn(vapidKey: String, done: (Result<JSONObject>) -> Unit)

    /** Turns reminders off; [done] gets the `push-off` answer. */
    fun pushOff(done: (Result<JSONObject>) -> Unit)
}

/**
 * The page side of the message bridge (docs/api.md, Android app). Each message
 * is a JSON string `{"id", "type", …}`; each gets one answer carrying the same
 * id, or `{"id", "error"}`. A message with no id cannot be answered and is
 * dropped. Every field is checked: an origin rule does not stop XSS on the hub.
 */
class Bridge(private val version: String, private val push: PushControl) {
    fun handle(raw: String, reply: (String) -> Unit) {
        val msg = try {
            JSONObject(raw)
        } catch (_: JSONException) {
            return
        }
        val id = msg.opt("id")
        if (id == null || id == JSONObject.NULL) return
        fun answer(body: JSONObject) {
            val out = JSONObject().put("id", id)
            for (k in body.keys()) if (k != "id") out.put(k, body.get(k))
            reply(out.toString())
        }
        fun fail(message: String) = reply(JSONObject().put("id", id).put("error", message).toString())
        fun deliver(result: Result<JSONObject>) =
            result.fold(::answer) { fail(it.message ?: "Something went wrong.") }

        when (msg.optString("type")) {
            "hello" -> answer(JSONObject().put("app", "android").put("version", version).put("bridge", BRIDGE_VERSION))
            "push-status" -> answer(push.status())
            "push-on" -> {
                val key = msg.opt("vapidKey") as? String
                if (key == null || !VAPID_KEY.matches(key)) {
                    fail("vapidKey must be the hub's VAPID public key.")
                    return
                }
                push.pushOn(key.trimEnd('='), ::deliver)
            }
            "push-off" -> push.pushOff(::deliver)
            else -> fail("Unknown message type.")
        }
    }

    companion object {
        const val BRIDGE_VERSION = 1

        /** The name the page sees, as `window.meatMode`. */
        const val JS_NAME = "meatMode"

        /** An uncompressed P-256 public key, base64url: 87 characters, as UnifiedPush needs. */
        private val VAPID_KEY = Regex("^[A-Za-z0-9_-]{87}=?$")
    }
}

/** Reminders as `push-status` reports them. */
object PushState {
    const val OFF = "off"
    const val WAITING = "waiting"
    const val ON = "on"
    const val BLOCKED = "blocked"
    const val UNAVAILABLE = "unavailable"

    /** The hub drops a registration not confirmed within 10 minutes. */
    const val CONFIRM_WINDOW_MS = 10 * 60 * 1000L

    fun of(
        hasDistributor: Boolean,
        endpoint: String?,
        hasToken: Boolean,
        notificationsEnabled: Boolean,
        registeredAt: Long,
        now: Long,
    ): String {
        val waiting = endpoint != null && !hasToken && now - registeredAt in 0..CONFIRM_WINDOW_MS
        if (!hasToken && !waiting) return if (hasDistributor) OFF else UNAVAILABLE
        if (!notificationsEnabled) return BLOCKED
        return if (hasToken) ON else WAITING
    }
}
