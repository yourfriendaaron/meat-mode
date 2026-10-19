package io.github.yourfriendaaron.meatmode

import android.content.Context
import android.os.Build
import java.io.File
import java.io.PrintWriter
import java.io.StringWriter
import java.time.Instant
import java.time.temporal.ChronoUnit

/**
 * The app's last crash, in release builds too: the stack trace, kept in a file
 * in the app's own storage, shown in Settings and never sent anywhere. Each
 * crash replaces the one before.
 */
object CrashLog {
    private const val FILE = "last-crash.txt"
    private const val MAX_CHARS = 32 * 1024

    /** Records an uncaught exception, then lets the previous handler end the process as before. */
    fun install(context: Context) {
        val app = context.applicationContext
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, e ->
            try {
                val device = "Android ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT})"
                File(app.filesDir, FILE).writeText(format(e, thread.name, BuildConfig.VERSION_NAME, device, Instant.now()))
            } catch (_: Throwable) {
                // Recording must never stand in the way of the crash itself.
            }
            previous?.uncaughtException(thread, e)
        }
    }

    /** The record of [e]: when, which build, which thread, and the stack trace with its causes. */
    fun format(e: Throwable, thread: String, version: String, device: String, at: Instant): String {
        val trace = StringWriter().also { e.printStackTrace(PrintWriter(it)) }.toString()
        val text = "${at.truncatedTo(ChronoUnit.SECONDS)}\nMeat Mode $version on $device\nThread: $thread\n\n$trace"
        return if (text.length > MAX_CHARS) text.take(MAX_CHARS) + "\n…" else text
    }

    fun read(context: Context): String? =
        File(context.filesDir, FILE).takeIf { it.exists() }?.readText()?.takeIf { it.isNotBlank() }

    fun clear(context: Context) {
        File(context.filesDir, FILE).delete()
    }
}
