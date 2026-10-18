package io.github.yourfriendaaron.meatmode

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class BridgeTest {
    private val vapid = "B" + "a".repeat(86)

    private class FakePush : PushControl {
        var onKey: String? = null
        var offCalls = 0
        var onResult: Result<JSONObject> = Result.success(
            JSONObject()
                .put("subscription", JSONObject().put("endpoint", "https://push.example.com/up/x").put("keys", JSONObject().put("p256dh", "p").put("auth", "a")))
                .put("name", "Test phone"),
        )

        override fun status(): JSONObject = JSONObject().put("state", "on").put("endpoint", "https://push.example.com/up/x")
            .put("lastPush", JSONObject.NULL).put("batteryExempt", true)

        override fun pushOn(vapidKey: String, done: (Result<JSONObject>) -> Unit) {
            onKey = vapidKey
            done(onResult)
        }

        override fun pushOff(done: (Result<JSONObject>) -> Unit) {
            offCalls++
            done(Result.success(JSONObject().put("state", "off")))
        }
    }

    private fun run(bridge: Bridge, msg: String): List<JSONObject> {
        val out = mutableListOf<JSONObject>()
        bridge.handle(msg) { out += JSONObject(it) }
        return out
    }

    @Test
    fun hello() {
        val r = run(Bridge("1.2.3", FakePush()), """{"id":"a1","type":"hello"}""").single()
        assertEquals("a1", r.getString("id"))
        assertEquals("android", r.getString("app"))
        assertEquals("1.2.3", r.getString("version"))
        assertEquals(1, r.getInt("bridge"))
    }

    @Test
    fun numericIdsComeBackAsSent() {
        val r = run(Bridge("1.2.3", FakePush()), """{"id":7,"type":"hello"}""").single()
        assertEquals(7, r.getInt("id"))
    }

    @Test
    fun pushStatus() {
        val r = run(Bridge("0.0.0", FakePush()), """{"id":"s","type":"push-status"}""").single()
        assertEquals("s", r.getString("id"))
        assertEquals("on", r.getString("state"))
        assertTrue(r.isNull("lastPush"))
        assertTrue(r.getBoolean("batteryExempt"))
    }

    @Test
    fun pushOnPassesTheKeyAndAnswersTheSubscription() {
        val push = FakePush()
        val r = run(Bridge("0.0.0", push), """{"id":"o","type":"push-on","vapidKey":"$vapid"}""").single()
        assertEquals(vapid, push.onKey)
        assertEquals("o", r.getString("id"))
        assertEquals("https://push.example.com/up/x", r.getJSONObject("subscription").getString("endpoint"))
        assertEquals("Test phone", r.getString("name"))
    }

    @Test
    fun pushOnFailureIsAnError() {
        val push = FakePush().apply { onResult = Result.failure(Registration.Failure("No push service was chosen.")) }
        val r = run(Bridge("0.0.0", push), """{"id":"o","type":"push-on","vapidKey":"$vapid"}""").single()
        assertEquals("No push service was chosen.", r.getString("error"))
        assertEquals("o", r.getString("id"))
    }

    @Test
    fun pushOnRefusesABadKey() {
        val push = FakePush()
        for (msg in listOf(
            """{"id":"o","type":"push-on"}""",
            """{"id":"o","type":"push-on","vapidKey":"short"}""",
            """{"id":"o","type":"push-on","vapidKey":42}""",
            """{"id":"o","type":"push-on","vapidKey":"${"+".repeat(87)}"}""",
        )) {
            val r = run(Bridge("0.0.0", push), msg).single()
            assertTrue(r.has("error"))
        }
        assertEquals(null, push.onKey)
    }

    @Test
    fun pushOff() {
        val push = FakePush()
        val r = run(Bridge("0.0.0", push), """{"id":"f","type":"push-off"}""").single()
        assertEquals("off", r.getString("state"))
        assertEquals(1, push.offCalls)
    }

    @Test
    fun unknownTypeIsAnError() {
        val r = run(Bridge("0.0.0", FakePush()), """{"id":"u","type":"open-settings"}""").single()
        assertEquals("Unknown message type.", r.getString("error"))
    }

    @Test
    fun messagesWithoutAnIdAreDropped() {
        val bridge = Bridge("0.0.0", FakePush())
        assertTrue(run(bridge, "not json").isEmpty())
        assertTrue(run(bridge, """{"type":"hello"}""").isEmpty())
        assertTrue(run(bridge, """{"id":null,"type":"hello"}""").isEmpty())
        assertTrue(run(bridge, "[1,2]").isEmpty())
    }

    @Test
    fun answersCannotOverrideTheId() {
        val push = object : PushControl {
            override fun status() = JSONObject().put("id", "spoofed").put("state", "off")
            override fun pushOn(vapidKey: String, done: (Result<JSONObject>) -> Unit) = Unit
            override fun pushOff(done: (Result<JSONObject>) -> Unit) = Unit
        }
        val r = run(Bridge("0.0.0", push), """{"id":"real","type":"push-status"}""").single()
        assertEquals("real", r.getString("id"))
    }
}
