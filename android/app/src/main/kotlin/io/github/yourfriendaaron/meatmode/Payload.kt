package io.github.yourfriendaaron.meatmode

import org.json.JSONException
import org.json.JSONObject
import java.time.Instant
import java.time.temporal.ChronoUnit

/** One button on a notification. */
data class PushButton(val action: String, val title: String)

/** What a push carries (docs/api.md, Agent push and Devices). */
sealed class Push {
    /** The hub's confirmation of a new registration; never shown. */
    data class Confirm(val device: String, val code: String) : Push()

    /** A reminder, an agent push or a test, shown as a notification. */
    data class Notice(
        val title: String,
        val body: String,
        val url: String,
        val tag: String?,
        val channel: String,
        val buttons: List<PushButton>,
        val key: String,
    ) : Push()
}

object Payload {
    const val CHANNEL_REMINDERS = "reminders"
    const val CHANNEL_AGENTS = "agents"
    const val CHANNEL_TEST = "test"

    const val ACTION_REPLY = "reply"
    const val ACTION_OPEN = "open"
    const val ACTION_RETRY = "retry"
    const val ACTION_DONE = "done"
    const val ACTION_SNOOZE = "snooze"

    /** Buttons that post an action in the background with the device token. */
    val BACKGROUND_ACTIONS = setOf(ACTION_RETRY, ACTION_DONE, ACTION_SNOOZE)
    private val KNOWN_ACTIONS = setOf(ACTION_REPLY, ACTION_OPEN) + BACKGROUND_ACTIONS

    /** Android shows at most three buttons on a notification. */
    const val MAX_BUTTONS = 3

    private val DEVICE_ID = Regex("^[0-9a-f]{32}$")
    private val CODE = Regex("^[A-Za-z0-9_-]{1,128}$")

    /**
     * Reads a decrypted push. A body that is not a JSON object becomes the text
     * of a plain notice, as in the service worker. A confirmation with a bad
     * device id or code is dropped (null), never shown.
     */
    fun parse(text: String): Push? {
        val obj = try {
            JSONObject(text)
        } catch (_: JSONException) {
            return Push.Notice("Meat Mode", text, "/", null, CHANNEL_REMINDERS, emptyList(), "")
        }
        val confirm = obj.optJSONObject("confirm")
        if (confirm != null) {
            val device = confirm.optString("device")
            val code = confirm.optString("code")
            if (!DEVICE_ID.matches(device) || !CODE.matches(code)) return null
            return Push.Confirm(device, code)
        }
        val key = obj.optString("key")
        val buttons = mutableListOf<PushButton>()
        val actions = obj.optJSONArray("actions")
        if (actions != null) {
            for (i in 0 until actions.length()) {
                val a = actions.optJSONObject(i) ?: continue
                val action = a.optString("action")
                val title = a.optString("title").trim()
                if (action !in KNOWN_ACTIONS || title.isEmpty()) continue
                // Every button but open acts on the item, so it needs the key.
                if (action != ACTION_OPEN && key.isEmpty()) continue
                buttons += PushButton(action, title)
                if (buttons.size == MAX_BUTTONS) break
            }
        }
        val channel = obj.optString("channel")
        return Push.Notice(
            title = obj.optString("title").ifBlank { "Meat Mode" },
            body = obj.optString("body"),
            url = obj.optString("url").ifBlank { "/" },
            tag = obj.optString("tag").ifBlank { null },
            channel = if (channel == CHANNEL_AGENTS || channel == CHANNEL_TEST) channel else CHANNEL_REMINDERS,
            buttons = buttons,
            key = key,
        )
    }

    /** The Android channel a push goes to: agents, or reminders (with test). */
    fun androidChannel(channel: String): String = if (channel == CHANNEL_AGENTS) CHANNEL_AGENTS else CHANNEL_REMINDERS

    /** The path a button that opens the app goes to. */
    fun openPath(notice: Push.Notice, action: String?): String =
        if (action == ACTION_REPLY && notice.key.isNotEmpty()) Hub.replyPath(notice.key) else notice.url

    /**
     * The `NewAction` a background button posts, as the service worker posts it:
     * retry asks again, done closes, snooze defers to an hour from [now].
     */
    fun actionBody(action: String, key: String, id: String, now: Instant): JSONObject {
        val body = JSONObject().put("id", id).put("key", key)
        when (action) {
            ACTION_RETRY -> body.put("kind", "ask").put("payload", JSONObject())
            ACTION_DONE -> body.put("kind", "done").put("payload", JSONObject())
            ACTION_SNOOZE -> body.put("kind", "snooze").put(
                "payload",
                JSONObject()
                    .put("until", now.truncatedTo(ChronoUnit.SECONDS).plus(1, ChronoUnit.HOURS).toString())
                    .put("previous", JSONObject.NULL),
            )
            else -> throw IllegalArgumentException("not a background action: $action")
        }
        return body
    }
}
