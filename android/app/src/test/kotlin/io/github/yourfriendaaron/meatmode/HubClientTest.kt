package io.github.yourfriendaaron.meatmode

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.IOException
import java.io.InputStream

class HubClientTest {
    /** Serves [size] bytes and counts how many were asked for. */
    private class Counting(private val size: Int) : InputStream() {
        var served = 0

        override fun read(): Int = if (served < size) { served++; 'a'.code } else -1

        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (served >= size) return -1
            val n = minOf(len, size - served)
            b.fill('a'.code.toByte(), off, off + n)
            served += n
            return n
        }
    }

    @Test
    fun aBodyUpToTheLimitIsRead() {
        val text = "a".repeat(HubClient.MAX_BODY)
        assertEquals(text, HubClient.readBounded(ByteArrayInputStream(text.toByteArray())))
        assertEquals("", HubClient.readBounded(ByteArrayInputStream(ByteArray(0))))
    }

    @Test
    fun anOversizedBodyIsRefusedWithoutReadingPastTheLimit() {
        val s = Counting(10 * HubClient.MAX_BODY)
        try {
            HubClient.readBounded(s)
            fail("expected a refusal")
        } catch (_: IOException) {
        }
        assertTrue(s.served <= HubClient.MAX_BODY + 1)
    }

    @Test
    fun failureTextMatchesTheWebApp() {
        assertEquals("The hub could not do that.", HubClient.failureText(500, ""))
        assertEquals("The hub could not do that.", HubClient.failureText(400, "not json"))
        assertEquals("The hub could not do that.", HubClient.failureText(400, """{"error":"  "}"""))
        assertEquals("The hub could not do that.", HubClient.failureText(400, """{"error":3}"""))
        assertEquals("Could not reach the hub.", HubClient.failureText(0, ""))
        assertEquals("Item not found.", HubClient.failureText(404, """{"error":"item not found"}"""))
        assertEquals("Already done!", HubClient.failureText(409, """{"error":" already done! "}"""))
    }
}
