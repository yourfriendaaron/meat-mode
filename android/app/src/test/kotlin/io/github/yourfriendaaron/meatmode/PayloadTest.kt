package io.github.yourfriendaaron.meatmode

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class PayloadTest {
    private val device = "0123456789abcdef0123456789abcdef"

    @Test
    fun confirmation() {
        val p = Payload.parse("""{"confirm":{"device":"$device","code":"AbC-_09"}}""")
        assertEquals(Push.Confirm(device, "AbC-_09"), p)
    }

    @Test
    fun confirmationWithBadFieldsIsDropped() {
        assertNull(Payload.parse("""{"confirm":{"device":"../x","code":"abc"}}"""))
        assertNull(Payload.parse("""{"confirm":{"device":"$device","code":"a/b"}}"""))
        assertNull(Payload.parse("""{"confirm":{"device":"$device"}}"""))
    }

    @Test
    fun reminderWithDoneAndSnooze() {
        val p = Payload.parse(
            """{"title":"Water the plants","body":"Due now","url":"/?item=hub%2Fpersonal%2Fmm-1","tag":"r-1",
               "channel":"reminders","key":"hub/personal/mm-1",
               "actions":[{"action":"done","title":"Done"},{"action":"snooze","title":"In 1 hour"}]}""",
        ) as Push.Notice
        assertEquals("Water the plants", p.title)
        assertEquals("Due now", p.body)
        assertEquals("/?item=hub%2Fpersonal%2Fmm-1", p.url)
        assertEquals("r-1", p.tag)
        assertEquals("reminders", p.channel)
        assertEquals(listOf(PushButton("done", "Done"), PushButton("snooze", "In 1 hour")), p.buttons)
        assertEquals("hub/personal/mm-1", p.key)
    }

    @Test
    fun agentPushGoesToAgents() {
        val p = Payload.parse(
            """{"title":"Sam replied","body":"b","url":"/?item=k","tag":"run-1","channel":"agents","key":"laptop/garden/gd-1",
               "actions":[{"action":"reply","title":"Reply"},{"action":"open","title":"Open"},{"action":"retry","title":"Retry"},{"action":"done","title":"Done"}]}""",
        ) as Push.Notice
        assertEquals("agents", Payload.androidChannel(p.channel))
        // Android shows three buttons at most.
        assertEquals(listOf("reply", "open", "retry"), p.buttons.map { it.action })
        assertEquals("/?item=laptop%2Fgarden%2Fgd-1&reply=1", Payload.openPath(p, "reply"))
        assertEquals("/?item=k", Payload.openPath(p, "open"))
        assertEquals("/?item=k", Payload.openPath(p, null))
    }

    @Test
    fun defaultsAndUnknowns() {
        val p = Payload.parse("""{"channel":"test","actions":[{"action":"delete","title":"Delete"},{"action":"done","title":"Done"},{"action":"open","title":""}]}""") as Push.Notice
        assertEquals("Meat Mode", p.title)
        assertEquals("", p.body)
        assertEquals("/", p.url)
        assertNull(p.tag)
        assertEquals("test", p.channel)
        assertEquals("reminders", Payload.androidChannel(p.channel))
        // Unknown actions, untitled buttons, and item buttons with no key are dropped.
        assertTrue(p.buttons.isEmpty())

        val other = Payload.parse("""{"channel":"something"}""") as Push.Notice
        assertEquals("reminders", other.channel)
    }

    @Test
    fun notJsonIsShownAsText() {
        val p = Payload.parse("plain words") as Push.Notice
        assertEquals("Meat Mode", p.title)
        assertEquals("plain words", p.body)
    }

    @Test
    fun actionBodies() {
        val now = Instant.parse("2026-03-02T09:15:42.500Z")
        val done = Payload.actionBody("done", "hub/personal/mm-1", "id-1", now)
        assertEquals("""{"id":"id-1","key":"hub/personal/mm-1","kind":"done","payload":{}}""", sorted(done))

        val retry = Payload.actionBody("retry", "laptop/garden/gd-1", "id-2", now)
        assertEquals("ask", retry.getString("kind"))
        assertEquals(0, retry.getJSONObject("payload").length())

        val snooze = Payload.actionBody("snooze", "hub/personal/mm-1", "id-3", now)
        assertEquals("snooze", snooze.getString("kind"))
        val payload = snooze.getJSONObject("payload")
        assertEquals("2026-03-02T10:15:42Z", payload.getString("until"))
        assertTrue(payload.isNull("previous"))
        assertTrue(payload.has("previous"))
    }

    private fun sorted(o: JSONObject): String {
        val keys = o.keys().asSequence().sorted().toList()
        return keys.joinToString(",", "{", "}") { k ->
            val v = o.get(k)
            "\"$k\":" + if (v is JSONObject) sorted(v) else JSONObject.quote(v.toString())
        }
    }
}
