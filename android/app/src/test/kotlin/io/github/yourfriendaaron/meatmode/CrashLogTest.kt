package io.github.yourfriendaaron.meatmode

import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class CrashLogTest {
    private val at = Instant.parse("2026-01-02T03:04:05.678Z")

    @Test
    fun recordsWhenWhereAndTheWholeTrace() {
        val e = IllegalStateException("outer", StringIndexOutOfBoundsException("length=0; index=0"))
        val text = CrashLog.format(e, "main", "1.2.3", "Android 15 (API 35)", at)
        assertTrue(text, text.startsWith("2026-01-02T03:04:05Z\nMeat Mode 1.2.3 on Android 15 (API 35)\nThread: main\n\n"))
        assertTrue(text, text.contains("java.lang.IllegalStateException: outer"))
        assertTrue(text, text.contains("Caused by: java.lang.StringIndexOutOfBoundsException: length=0; index=0"))
        assertTrue(text, text.contains("CrashLogTest.recordsWhenWhereAndTheWholeTrace"))
    }

    @Test
    fun keepsAHugeTraceBounded() {
        val text = CrashLog.format(RuntimeException("x".repeat(100_000)), "main", "1.2.3", "Android 15 (API 35)", at)
        assertTrue(text.length < 33 * 1024)
        assertTrue(text.endsWith("…"))
    }
}
