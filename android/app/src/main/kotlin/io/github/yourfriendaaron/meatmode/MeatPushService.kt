package io.github.yourfriendaaron.meatmode

import org.unifiedpush.android.connector.FailedReason
import org.unifiedpush.android.connector.PushService
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage

/** Receives UnifiedPush events; the connector has already decrypted each message. */
class MeatPushService : PushService() {
    override fun onNewEndpoint(endpoint: PushEndpoint, instance: String) {
        Registration.onNewEndpoint(this, endpoint.url, endpoint.pubKeySet?.pubKey, endpoint.pubKeySet?.auth)
    }

    override fun onMessage(message: PushMessage, instance: String) {
        val store = Store(this)
        store.lastPush = System.currentTimeMillis()
        if (!message.decrypted) {
            PushLog.add(this, "push of ${message.content.size} bytes that did not decrypt: dropped")
            return
        }
        when (val push = Payload.parse(message.content.toString(Charsets.UTF_8))) {
            is Push.Confirm -> {
                PushLog.add(this, "confirmation push")
                Registration.onConfirm(this, push.device, push.code)
            }
            is Push.Notice -> {
                PushLog.add(this, "push on ${push.channel}, tag ${push.tag ?: "none"}, ${push.buttons.size} buttons")
                if (store.origin != null) Notifier.show(this, push)
            }
            null -> PushLog.add(this, "malformed confirmation push: dropped")
        }
    }

    override fun onRegistrationFailed(reason: FailedReason, instance: String) {
        Registration.onRegistrationFailed(this, reason)
    }

    override fun onUnregistered(instance: String) {
        Registration.onUnregistered(this)
    }

    override fun onTempUnavailable(instance: String) {
        PushLog.add(this, "push service temporarily unavailable")
    }
}
