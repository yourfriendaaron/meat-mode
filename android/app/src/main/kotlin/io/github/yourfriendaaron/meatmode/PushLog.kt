package io.github.yourfriendaaron.meatmode

import android.content.Context
import java.io.File
import java.time.Instant
import java.time.temporal.ChronoUnit

/**
 * Recent push events for the debug build's log screen, kept in a file in the
 * app's own storage and never sent anywhere. Release builds write nothing.
 * Entries name hosts and HTTP codes only: no endpoint path, token or code.
 */
object PushLog {
    private const val FILE = "push-log.txt"
    private const val KEEP = 200

    @Synchronized
    fun add(context: Context, event: String) {
        if (!BuildConfig.DEBUG) return
        val file = File(context.filesDir, FILE)
        val lines = if (file.exists()) file.readLines() else emptyList()
        val line = Instant.now().truncatedTo(ChronoUnit.SECONDS).toString() + "  " + event.replace('\n', ' ')
        file.writeText((lines.takeLast(KEEP - 1) + line).joinToString("\n", postfix = "\n"))
    }

    /** Newest first. */
    @Synchronized
    fun read(context: Context): List<String> {
        val file = File(context.filesDir, FILE)
        return if (file.exists()) file.readLines().filter { it.isNotBlank() }.reversed() else emptyList()
    }

    @Synchronized
    fun clear(context: Context) {
        File(context.filesDir, FILE).delete()
    }

    /** An endpoint shown by host only: the rest of it is a capability URL. */
    fun host(url: String?): String = url?.let { Hub.originOf(it) } ?: "none"
}
