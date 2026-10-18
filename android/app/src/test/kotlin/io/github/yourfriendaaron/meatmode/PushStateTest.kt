package io.github.yourfriendaaron.meatmode

import org.junit.Assert.assertEquals
import org.junit.Test

class PushStateTest {
    private val now = 1_000_000_000L
    private val ep = "https://push.example.com/up/x"

    private fun state(dist: Boolean = true, endpoint: String? = null, token: Boolean = false, notif: Boolean = true, at: Long = now) =
        PushState.of(dist, endpoint, token, notif, at, now)

    @Test
    fun offOrUnavailableWithNoRegistration() {
        assertEquals("off", state())
        assertEquals("unavailable", state(dist = false))
        // Before Android 13's permission prompt, notifications read as off: that is still "off".
        assertEquals("off", state(notif = false))
    }

    @Test
    fun waitingUntilConfirmedWithinTenMinutes() {
        assertEquals("waiting", state(endpoint = ep, at = now - 60_000))
        assertEquals("off", state(endpoint = ep, at = now - PushState.CONFIRM_WINDOW_MS - 1))
    }

    @Test
    fun onWithAToken() {
        assertEquals("on", state(endpoint = ep, token = true, at = 0))
    }

    @Test
    fun blockedWhenRegisteredButNotificationsAreOff() {
        assertEquals("blocked", state(endpoint = ep, token = true, notif = false))
        assertEquals("blocked", state(endpoint = ep, notif = false))
    }
}
