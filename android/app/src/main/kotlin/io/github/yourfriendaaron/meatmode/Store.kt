package io.github.yourfriendaaron.meatmode

import android.content.Context
import android.content.SharedPreferences
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import androidx.core.content.edit
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** The app's own settings: the hub address and the push registration. Not backed up. */
class Store(context: Context) {
    private val prefs: SharedPreferences = context.applicationContext.getSharedPreferences("meat-mode", Context.MODE_PRIVATE)

    var origin: String?
        get() = prefs.getString("origin", null)
        set(v) = prefs.edit { putString("origin", v) }

    /** The UnifiedPush endpoint and its Web Push keys, once the distributor gave them. */
    val endpoint: String? get() = prefs.getString("endpoint", null)
    val p256dh: String? get() = prefs.getString("p256dh", null)
    val auth: String? get() = prefs.getString("auth", null)

    fun setEndpoint(url: String, p256dh: String, auth: String) = prefs.edit {
        putString("endpoint", url)
        putString("p256dh", p256dh)
        putString("auth", auth)
    }

    /** The hub's VAPID public key from the last push-on, for registering again. */
    var vapidKey: String?
        get() = prefs.getString("vapidKey", null)
        set(v) = prefs.edit { putString("vapidKey", v) }

    /** When push-on last registered, for the hub's 10-minute confirmation window. */
    var registeredAt: Long
        get() = prefs.getLong("registeredAt", 0)
        set(v) = prefs.edit { putLong("registeredAt", v) }

    /** The confirmed device's id, from the confirmation push. */
    var deviceId: String?
        get() = prefs.getString("deviceId", null)
        set(v) = prefs.edit { putString("deviceId", v) }

    /** When the last push arrived, in epoch milliseconds; 0 before the first. */
    var lastPush: Long
        get() = prefs.getLong("lastPush", 0)
        set(v) = prefs.edit { putLong("lastPush", v) }

    /** Whether the battery exemption was asked for already; the app asks once. */
    var batteryAsked: Boolean
        get() = prefs.getBoolean("batteryAsked", false)
        set(v) = prefs.edit { putBoolean("batteryAsked", v) }

    /** Drops the registration: endpoint, keys and device id. The token is in [Secrets]. */
    fun clearPush() = prefs.edit {
        remove("endpoint")
        remove("p256dh")
        remove("auth")
        remove("registeredAt")
        remove("deviceId")
        remove("vapidKey")
    }
}

/**
 * Secrets kept encrypted with an AES key in the Android Keystore: the device
 * token, a confirmation code until it is used, and a token waiting to be
 * revoked. A value that no longer decrypts (the key was lost) reads as absent.
 */
class Secrets(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("meat-mode-secrets", Context.MODE_PRIVATE)

    fun get(slot: String): String? {
        val stored = prefs.getString(slot, null) ?: return null
        return try {
            val (iv, data) = stored.split(":", limit = 2).map { Base64.decode(it, Base64.NO_WRAP) }
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, iv))
            String(cipher.doFinal(data), Charsets.UTF_8)
        } catch (_: Exception) {
            prefs.edit { remove(slot) }
            null
        }
    }

    fun set(slot: String, value: String?) {
        if (value == null) {
            prefs.edit { remove(slot) }
            return
        }
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val data = cipher.doFinal(value.toByteArray(Charsets.UTF_8))
        val stored = Base64.encodeToString(cipher.iv, Base64.NO_WRAP) + ":" + Base64.encodeToString(data, Base64.NO_WRAP)
        prefs.edit { putString(slot, stored) }
    }

    private fun key(): SecretKey {
        val ks = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (ks.getKey(ALIAS, null) as? SecretKey)?.let { return it }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        gen.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return gen.generateKey()
    }

    companion object {
        const val TOKEN = "device-token"
        const val CONFIRM_CODE = "confirm-code"
        const val REVOKE_TOKEN = "revoke-token"

        private const val KEYSTORE = "AndroidKeyStore"
        private const val ALIAS = "meat-mode-secrets"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
    }
}
