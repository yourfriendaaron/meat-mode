package io.github.yourfriendaaron.meatmode

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import java.time.Instant
import java.util.UUID

/**
 * Takes a notification's retry, done or snooze button. The action is built
 * here, so its id and snooze time are fixed at the tap, and posted by
 * [ActionWorker]; a retry of the same id changes nothing on the hub.
 */
class ActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION) return
        val tag = intent.getStringExtra(EXTRA_TAG) ?: return
        val button = intent.getStringExtra(EXTRA_BUTTON) ?: return
        val key = intent.getStringExtra(EXTRA_KEY) ?: return
        if (button !in Payload.BACKGROUND_ACTIONS || key.isEmpty()) return
        Notifier.cancel(context, tag)
        val body = Payload.actionBody(button, key, UUID.randomUUID().toString(), Instant.now())
        Work.action(context, tag, button, key, body.toString())
    }

    companion object {
        const val ACTION = "io.github.yourfriendaaron.meatmode.NOTIFICATION_ACTION"
        const val EXTRA_TAG = "tag"
        const val EXTRA_BUTTON = "button"
        const val EXTRA_KEY = "key"
    }
}
