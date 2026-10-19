package io.github.yourfriendaaron.meatmode

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PageColorTest {
    @Test
    fun readsTheHubsThemeColours() {
        assertEquals(0xFF282A36.toInt(), PageColor.parse("\"#282A36\""))
        assertEquals(0xFFFFFBEB.toInt(), PageColor.parse("\"#fffbeb\""))
        assertEquals(0xFFAABBCC.toInt(), PageColor.parse("\"#abc\""))
    }

    @Test
    fun aPageWithoutItsHeadYetHasNoColour() {
        // The launch crash: a new page answered "" before its meta tag was parsed.
        assertNull(PageColor.parse("\"\""))
        assertNull(PageColor.parse("null"))
        assertNull(PageColor.parse(""))
        assertNull(PageColor.parse(null))
    }

    @Test
    fun anythingButHexIsNoColour() {
        for (r in listOf("\"red\"", "\"#12\"", "\"#1234567\"", "\"#GGGGGG\"", "\"rgb(1,2,3)\"", "42", "{")) {
            assertNull(r, PageColor.parse(r))
        }
    }
}
