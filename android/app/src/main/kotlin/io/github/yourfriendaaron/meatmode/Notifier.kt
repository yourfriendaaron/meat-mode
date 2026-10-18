package io.github.yourfriendaaron.meatmode

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat

/** Builds notifications from push payloads, on the reminders and agents channels. */
object Notifier {
    private const val ID = 1
    private const val COLOR = 0xFFFF79C6.toInt()

    fun ensureChannels(context: Context) {
        val nm = context.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(Payload.CHANNEL_REMINDERS, context.getString(R.string.channel_reminders), NotificationManager.IMPORTANCE_HIGH)
                .apply { description = context.getString(R.string.channel_reminders_about) },
        )
        nm.createNotificationChannel(
            NotificationChannel(Payload.CHANNEL_AGENTS, context.getString(R.string.channel_agents), NotificationManager.IMPORTANCE_DEFAULT)
                .apply { description = context.getString(R.string.channel_agents_about) },
        )
    }

    fun show(context: Context, notice: Push.Notice) {
        ensureChannels(context)
        val tag = notice.tag ?: "push-${System.currentTimeMillis()}"
        val b = NotificationCompat.Builder(context, Payload.androidChannel(notice.channel))
            .setSmallIcon(R.drawable.ic_notification)
            .setColor(COLOR)
            .setContentTitle(notice.title)
            .setContentText(notice.body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(notice.body))
            .setAutoCancel(true)
            .setContentIntent(openIntent(context, notice.url, "$tag#tap"))
        for (button in notice.buttons) {
            val pi = if (button.action in Payload.BACKGROUND_ACTIONS) {
                actionIntent(context, tag, button.action, notice.key)
            } else {
                openIntent(context, Payload.openPath(notice, button.action), "$tag#${button.action}")
            }
            b.addAction(NotificationCompat.Action.Builder(0, button.title, pi).build())
        }
        post(context, tag, b)
    }

    /** Replaces a notification whose button the hub refused with one that opens the item. */
    fun showRefused(context: Context, tag: String, key: String, message: String) {
        ensureChannels(context)
        val b = NotificationCompat.Builder(context, Payload.CHANNEL_REMINDERS)
            .setSmallIcon(R.drawable.ic_notification)
            .setColor(COLOR)
            .setContentTitle(context.getString(R.string.action_refused_title))
            .setContentText(message)
            .setAutoCancel(true)
            .setContentIntent(openIntent(context, Hub.itemPath(key), "$tag#refused"))
        post(context, tag, b)
    }

    fun cancel(context: Context, tag: String) = NotificationManagerCompat.from(context).cancel(tag, ID)

    private fun post(context: Context, tag: String, b: NotificationCompat.Builder) {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED &&
            android.os.Build.VERSION.SDK_INT >= 33
        ) {
            PushLog.add(context, "notification not shown: permission not granted")
            return
        }
        NotificationManagerCompat.from(context).notify(tag, ID, b.build())
    }

    private fun openIntent(context: Context, path: String, unique: String): PendingIntent {
        val intent = Intent(context, MainActivity::class.java)
            .setAction(MainActivity.ACTION_OPEN)
            .putExtra(MainActivity.EXTRA_PATH, path)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        return PendingIntent.getActivity(context, unique.hashCode(), intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }

    private fun actionIntent(context: Context, tag: String, action: String, key: String): PendingIntent {
        val intent = Intent(context, ActionReceiver::class.java)
            .setAction(ActionReceiver.ACTION)
            .putExtra(ActionReceiver.EXTRA_TAG, tag)
            .putExtra(ActionReceiver.EXTRA_BUTTON, action)
            .putExtra(ActionReceiver.EXTRA_KEY, key)
        return PendingIntent.getBroadcast(context, "$tag#$action".hashCode(), intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }
}
