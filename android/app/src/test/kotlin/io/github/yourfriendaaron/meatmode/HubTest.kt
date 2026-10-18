package io.github.yourfriendaaron.meatmode

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HubTest {
    private fun ok(input: String, dev: Boolean = false) = (Hub.parseAddress(input, dev) as Hub.Address.Ok).origin
    private fun bad(input: String, dev: Boolean = false) = Hub.parseAddress(input, dev) is Hub.Address.Bad

    @Test
    fun addressBecomesAnHttpsOrigin() {
        assertEquals("https://hub.example.com", ok("hub.example.com"))
        assertEquals("https://hub.example.com", ok("  https://Hub.Example.com/  "))
        assertEquals("https://hub.example.com", ok("https://hub.example.com:443"))
        assertEquals("https://hub.example.com:8443", ok("https://hub.example.com:8443/some/path?item=x#top"))
        assertEquals("https://[2001:db8::1]", ok("https://[2001:db8::1]/"))
        assertEquals("https://192.0.2.10:8080", ok("192.0.2.10:8080"))
    }

    @Test
    fun addressRefusesOtherSchemesAndUserInfo() {
        assertTrue(bad(""))
        assertTrue(bad("http://hub.example.com"))
        assertTrue(bad("ftp://hub.example.com"))
        assertTrue(bad("javascript:alert(1)"))
        assertTrue(bad("https://alex:secret@example.com"))
        assertTrue(bad("https://"))
        assertTrue(bad("https://hub example.com"))
    }

    @Test
    fun plainHttpOnlyForDevHostsInDebug() {
        assertTrue(bad("http://localhost:8080"))
        assertEquals("http://localhost:8080", ok("http://localhost:8080", dev = true))
        assertEquals("http://127.0.0.1:8080", ok("http://127.0.0.1:8080", dev = true))
        assertEquals("http://${Hub.EMULATOR_HOST}:8080", ok("http://${Hub.EMULATOR_HOST}:8080", dev = true))
        assertTrue(bad("http://hub.example.com", dev = true))
        assertEquals("http://localhost", ok("http://localhost:80", dev = true))
    }

    @Test
    fun originRules() {
        val o = "https://hub.example.com"
        assertTrue(Hub.isSameOrigin("https://hub.example.com/?item=a", o))
        assertTrue(Hub.isSameOrigin("https://HUB.example.com:443/x", o))
        assertFalse(Hub.isSameOrigin("http://hub.example.com/", o))
        assertFalse(Hub.isSameOrigin("https://hub.example.com:8443/", o))
        assertFalse(Hub.isSameOrigin("https://hub.example.com.example.org/", o))
        assertFalse(Hub.isSameOrigin("https://other.example.com/", o))
        assertFalse(Hub.isSameOrigin("mailto:sam@example.com", o))
        assertFalse(Hub.isSameOrigin("not a url", o))
    }

    @Test
    fun inAppUrlStaysOnTheHub() {
        val o = "https://hub.example.com"
        assertEquals("https://hub.example.com/", Hub.inAppUrl(null, o))
        assertEquals("https://hub.example.com/", Hub.inAppUrl("", o))
        assertEquals("https://hub.example.com/?item=x", Hub.inAppUrl("/?item=x", o))
        assertEquals("https://hub.example.com/?item=x", Hub.inAppUrl("https://hub.example.com/?item=x", o))
        assertEquals("https://hub.example.com/", Hub.inAppUrl("https://other.example.com/?item=x", o))
        assertEquals("https://hub.example.com/", Hub.inAppUrl("//other.example.com/x", o))
        assertEquals("https://hub.example.com/", Hub.inAppUrl("javascript:alert(1)", o))
        assertEquals("https://hub.example.com/", Hub.inAppUrl("http://hub.example.com/", o))
    }

    @Test
    fun itemPathsMatchTheServiceWorker() {
        // encodeURIComponent("laptop/projects/garden/gd-1")
        assertEquals("/?item=laptop%2Fprojects%2Fgarden%2Fgd-1", Hub.itemPath("laptop/projects/garden/gd-1"))
        assertEquals("/?item=hub%2Fpersonal%2Fmm-3&reply=1", Hub.replyPath("hub/personal/mm-3"))
        assertEquals("a%20b!~*'()%C3%A9%26", Hub.encodeURIComponent("a b!~*'()é&"))
    }
}
