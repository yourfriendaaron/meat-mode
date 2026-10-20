package io.github.yourfriendaaron.meatmode

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CaptureTest {
    @Test
    fun anInputWithoutTypesTakesAPhoto() {
        assertTrue(Capture.acceptsImages(null))
        assertTrue(Capture.acceptsImages(arrayOf()))
        assertTrue(Capture.acceptsImages(arrayOf("")))
        assertTrue(Capture.acceptsImages(arrayOf(" ", "")))
    }

    @Test
    fun imageTypesTakeAPhoto() {
        assertTrue(Capture.acceptsImages(arrayOf("image/*")))
        assertTrue(Capture.acceptsImages(arrayOf("image/png")))
        assertTrue(Capture.acceptsImages(arrayOf("IMAGE/JPEG")))
        assertTrue(Capture.acceptsImages(arrayOf(".jpg")))
        assertTrue(Capture.acceptsImages(arrayOf("application/pdf", "image/*")))
        assertTrue(Capture.acceptsImages(arrayOf("application/pdf, image/*")))
    }

    @Test
    fun otherTypesKeepThePicker() {
        assertFalse(Capture.acceptsImages(arrayOf("application/pdf")))
        assertFalse(Capture.acceptsImages(arrayOf("video/*")))
        assertFalse(Capture.acceptsImages(arrayOf(".txt", "text/plain")))
        assertFalse(Capture.acceptsImages(arrayOf("application/json,.csv")))
    }
}
