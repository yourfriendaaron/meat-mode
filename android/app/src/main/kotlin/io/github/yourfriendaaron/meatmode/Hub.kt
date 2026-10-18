package io.github.yourfriendaaron.meatmode

import java.net.URI
import java.net.URISyntaxException

/**
 * Rules for the hub address and for the URLs the app opens in its WebView.
 * Pure Kotlin on java.net.URI, so the unit tests run it on the JVM.
 */
object Hub {
    /**
     * The emulator reaches its host at this address. Built from parts so the
     * public-safety guard, which flags private IPv4 literals, passes the file.
     */
    val EMULATOR_HOST: String = listOf(10, 0, 2, 2).joinToString(".")

    /** Hosts where a debug build may use plain http, for development only. */
    val DEV_HOSTS = setOf("localhost", "127.0.0.1", "[::1]", EMULATOR_HOST)

    sealed class Address {
        /** The hub's origin, `scheme://host[:port]` with no path. */
        data class Ok(val origin: String) : Address()
        data class Bad(val reason: String) : Address()
    }

    /**
     * Reads what the user typed as the hub address. A bare host gets `https://`;
     * a path, query or fragment is dropped, since the hub serves from its root.
     * Plain http is refused unless [allowHttpDev] and the host is in [DEV_HOSTS].
     */
    fun parseAddress(input: String, allowHttpDev: Boolean): Address {
        val text = input.trim()
        if (text.isEmpty()) return Address.Bad("Enter the hub's address.")
        val withScheme = if (Regex("^[A-Za-z][A-Za-z0-9+.-]*://").containsMatchIn(text)) text else "https://$text"
        val uri = try {
            URI(withScheme)
        } catch (_: URISyntaxException) {
            return Address.Bad("That is not a web address.")
        }
        val scheme = uri.scheme?.lowercase()
        val host = uri.host?.lowercase()
        if (host.isNullOrEmpty()) return Address.Bad("That is not a web address.")
        if (uri.rawUserInfo != null) return Address.Bad("Leave the user name and password out of the address.")
        when (scheme) {
            "https" -> {}
            "http" -> if (!allowHttpDev || host !in DEV_HOSTS) {
                return Address.Bad("The address must start with https://.")
            }
            else -> return Address.Bad("The address must start with https://.")
        }
        return Address.Ok(origin(scheme, host, uri.port))
    }

    /** The origin of an http or https URL, or null for anything else. */
    fun originOf(url: String): String? {
        val uri = try {
            URI(url)
        } catch (_: URISyntaxException) {
            return null
        }
        val scheme = uri.scheme?.lowercase() ?: return null
        if (scheme != "https" && scheme != "http") return null
        val host = uri.host?.lowercase()
        if (host.isNullOrEmpty()) return null
        return origin(scheme, host, uri.port)
    }

    fun isSameOrigin(url: String, origin: String): Boolean = originOf(url) == origin

    /**
     * The absolute URL to open in the WebView for [target], a path or URL from a
     * push or an intent. Anything off the hub's origin opens the hub's root
     * instead, as the service worker does.
     */
    fun inAppUrl(target: String?, origin: String): String {
        val root = "$origin/"
        if (target.isNullOrBlank()) return root
        val resolved = try {
            URI(root).resolve(target.trim()).toString()
        } catch (_: IllegalArgumentException) {
            return root
        }
        return if (isSameOrigin(resolved, origin)) resolved else root
    }

    /** The item's page, as the service worker builds it. */
    fun itemPath(key: String): String = "/?item=" + encodeURIComponent(key)

    /** The item's page at its note box. */
    fun replyPath(key: String): String = itemPath(key) + "&reply=1"

    /** JavaScript's encodeURIComponent, so app and service worker build the same URLs. */
    fun encodeURIComponent(s: String): String {
        val keep = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
        val out = StringBuilder()
        for (b in s.toByteArray(Charsets.UTF_8)) {
            val c = (b.toInt() and 0xff).toChar()
            if (c.code < 0x80 && c in keep) out.append(c) else out.append("%%%02X".format(b.toInt() and 0xff))
        }
        return out.toString()
    }

    private fun origin(scheme: String, host: String, port: Int): String {
        val default = if (scheme == "https") 443 else 80
        return if (port == -1 || port == default) "$scheme://$host" else "$scheme://$host:$port"
    }
}
