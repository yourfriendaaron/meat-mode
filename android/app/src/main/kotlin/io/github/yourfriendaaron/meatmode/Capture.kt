package io.github.yourfriendaaron.meatmode

/**
 * Decides when a file input opens the camera instead of the picker. Pure
 * Kotlin, so the unit tests run it on the JVM.
 */
object Capture {
    private val imageExtensions = setOf(".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif", ".avif", ".bmp")

    /**
     * True when an input with these accept types takes a photo: no types at
     * all, or any image type (all images or one subtype) or image file extension.
     * WebView may hand the accept attribute over whole or already split at
     * its commas, so both shapes are read.
     */
    fun acceptsImages(acceptTypes: Array<String>?): Boolean {
        val types = acceptTypes.orEmpty()
            .flatMap { it.split(',') }
            .map { it.trim().lowercase() }
            .filter { it.isNotEmpty() }
        if (types.isEmpty()) return true
        return types.any { it == "image/*" || it.startsWith("image/") || it in imageExtensions }
    }
}
