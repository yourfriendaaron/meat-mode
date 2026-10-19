package io.github.yourfriendaaron.meatmode

import org.json.JSONArray
import org.json.JSONException

/**
 * Reads the page's theme colour from what `evaluateJavascript` returns. Pure
 * Kotlin, so the unit tests run it on the JVM.
 */
object PageColor {
    /**
     * The colour in [result], a JSON value, as an opaque ARGB int; null for
     * anything but `#rgb` or `#rrggbb`. The result is an empty string while a
     * new page has not parsed its head yet, and Color.parseColor throws on that
     * with an exception that is not an IllegalArgumentException.
     */
    fun parse(result: String?): Int? {
        val value = try {
            JSONArray("[${result.orEmpty()}]").opt(0) as? String
        } catch (_: JSONException) {
            null
        } ?: return null
        val hex = Regex("^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$").matchEntire(value.trim())?.groupValues?.get(1) ?: return null
        val full = if (hex.length == 3) hex.map { "$it$it" }.joinToString("") else hex
        return (0xFF000000 or full.toLong(16)).toInt()
    }
}
