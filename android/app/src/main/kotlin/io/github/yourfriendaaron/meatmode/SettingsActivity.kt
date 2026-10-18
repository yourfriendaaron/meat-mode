package io.github.yourfriendaaron.meatmode

import android.app.AlertDialog
import android.content.Intent
import android.os.Bundle
import android.provider.Settings
import android.text.format.DateUtils
import android.view.View
import android.webkit.CookieManager
import android.webkit.WebStorage
import android.widget.Button
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.activity.enableEdgeToEdge

/**
 * The app's own settings, reached from the launcher icon's shortcut, Android's
 * app info screen, and the offline page: the hub address, how reminders
 * stand, and when the last push arrived.
 */
class SettingsActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_settings)
        padForSystemBars(findViewById(R.id.root))

        findViewById<Button>(R.id.change_hub).setOnClickListener { confirmChangeHub() }
        findViewById<Button>(R.id.notification_settings).setOnClickListener {
            startActivity(Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, packageName))
        }
        findViewById<Button>(R.id.battery).setOnClickListener { requestBatteryExemption(this) }
        findViewById<Button>(R.id.push_log).apply {
            // The log screen exists in debug builds only.
            visibility = if (BuildConfig.DEBUG) View.VISIBLE else View.GONE
            setOnClickListener { startActivity(Intent().setClassName(packageName, "$packageName.PushLogActivity")) }
        }
    }

    override fun onResume() {
        super.onResume()
        val store = Store(this)
        val status = Registration.status(this)
        findViewById<TextView>(R.id.hub).text = store.origin ?: getString(R.string.settings_no_hub)
        findViewById<TextView>(R.id.reminders).text = when (status.optString("state")) {
            PushState.ON -> getString(R.string.state_on)
            PushState.WAITING -> getString(R.string.state_waiting)
            PushState.BLOCKED -> getString(R.string.state_blocked)
            PushState.UNAVAILABLE -> getString(R.string.state_unavailable)
            else -> getString(R.string.state_off)
        }
        findViewById<TextView>(R.id.distributor).text = Registration.distributorName(this) ?: getString(R.string.settings_none)
        findViewById<TextView>(R.id.last_push).text = if (store.lastPush > 0) {
            DateUtils.getRelativeDateTimeString(this, store.lastPush, DateUtils.MINUTE_IN_MILLIS, DateUtils.WEEK_IN_MILLIS, 0)
        } else {
            getString(R.string.settings_never)
        }
        val exempt = Registration.batteryExempt(this)
        findViewById<TextView>(R.id.battery_state).text = getString(if (exempt) R.string.battery_unrestricted else R.string.battery_optimised)
        findViewById<Button>(R.id.battery).visibility = if (exempt) View.GONE else View.VISIBLE
    }

    private fun confirmChangeHub() {
        AlertDialog.Builder(this)
            .setTitle(R.string.change_hub_title)
            .setMessage(R.string.change_hub_message)
            .setPositiveButton(R.string.change_hub_confirm) { _, _ -> changeHub() }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /** Turns reminders off on the old hub, clears its cookies and storage, and starts setup again. */
    private fun changeHub() {
        val store = Store(this)
        val previous = store.origin
        Registration.forget(this, revoke = true)
        CookieManager.getInstance().removeAllCookies(null)
        CookieManager.getInstance().flush()
        WebStorage.getInstance().deleteAllData()
        store.origin = null
        startActivity(
            Intent(this, SetupActivity::class.java)
                .putExtra(SetupActivity.EXTRA_PREVIOUS, previous)
                .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TASK or Intent.FLAG_ACTIVITY_NEW_TASK),
        )
        finish()
    }
}
