package io.github.yourfriendaaron.meatmode

import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.net.UnknownHostException
import javax.net.ssl.SSLException

/**
 * Plain HTTP calls to the hub from outside the WebView: the address check, and
 * the device-token calls. Blocking; call it off the main thread. A request
 * carries no Origin and no cookie, and every body is JSON (docs/api.md, Devices).
 */
class HubClient(private val origin: String) {
    data class Response(val code: Int, val body: String)

    fun get(path: String): Response = send("GET", path, null, null)

    fun send(method: String, path: String, body: String?, token: String?): Response {
        val conn = URL(origin + path).openConnection() as HttpURLConnection
        try {
            conn.requestMethod = method
            conn.connectTimeout = 10_000
            conn.readTimeout = 15_000
            // A redirect could carry the token elsewhere; the hub never sends one.
            conn.instanceFollowRedirects = false
            conn.useCaches = false
            conn.setRequestProperty("Accept", "application/json")
            if (token != null) conn.setRequestProperty("Authorization", "Bearer $token")
            if (method != "GET") {
                conn.setRequestProperty("Content-Type", "application/json")
                if (body != null) {
                    conn.doOutput = true
                    conn.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
                }
            }
            val code = conn.responseCode
            val stream = if (code >= 400) conn.errorStream else conn.inputStream
            val text = stream?.use { s -> s.readBytes().take(MAX_BODY).toByteArray().toString(Charsets.UTF_8) }.orEmpty()
            return Response(code, text)
        } finally {
            conn.disconnect()
        }
    }

    sealed class Check {
        data object Ok : Check()
        data class Failed(val reason: String) : Check()
    }

    /** Checks that [origin] is a Meat Mode hub: `/healthz` answers and `/api/session` reads as one. */
    fun check(): Check = try {
        val health = get("/healthz")
        if (health.code !in 200..299) {
            Check.Failed("The hub answered ${health.code} to its health check.")
        } else {
            val session = get("/api/session")
            val obj = try {
                JSONObject(session.body)
            } catch (_: JSONException) {
                null
            }
            if (session.code == 200 && obj != null && obj.opt("login") is Boolean && obj.opt("authenticated") is Boolean) {
                Check.Ok
            } else {
                Check.Failed("That address answers, but not as a Meat Mode hub.")
            }
        }
    } catch (_: UnknownHostException) {
        Check.Failed("No server answers at that address. Check it, and that this phone is on the hub's network.")
    } catch (_: SSLException) {
        Check.Failed("The hub's certificate is not trusted on this phone.")
    } catch (e: IOException) {
        Check.Failed("Could not reach the hub: ${e.message ?: "no answer"}.")
    }

    companion object {
        private const val MAX_BODY = 64 * 1024
    }
}
