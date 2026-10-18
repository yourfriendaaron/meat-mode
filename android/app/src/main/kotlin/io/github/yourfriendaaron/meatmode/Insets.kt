package io.github.yourfriendaaron.meatmode

import android.view.View
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

/**
 * Pads [view] clear of the system bars, the display cutout and the keyboard,
 * and consumes the insets so nothing inside (the WebView's own safe-area and
 * keyboard handling included) applies them a second time.
 */
fun padForSystemBars(view: View) {
    ViewCompat.setOnApplyWindowInsetsListener(view) { v, insets ->
        val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
        val ime = insets.getInsets(WindowInsetsCompat.Type.ime())
        v.setPadding(bars.left, bars.top, bars.right, maxOf(bars.bottom, ime.bottom))
        WindowInsetsCompat.CONSUMED
    }
}
