package io.github.yourfriendaaron.meatmode

import android.content.Intent
import android.os.Bundle
import android.view.KeyEvent
import android.view.View
import android.view.inputmethod.EditorInfo
import android.widget.Button
import android.widget.EditText
import android.widget.ProgressBar
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.activity.enableEdgeToEdge

/**
 * First run, and changing the hub: asks for the hub's address and saves it
 * only once `/healthz` and `/api/session` answer as a Meat Mode hub.
 */
class SetupActivity : ComponentActivity() {
    private lateinit var address: EditText
    private lateinit var connect: Button
    private lateinit var status: TextView
    private lateinit var progress: ProgressBar

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_setup)
        padForSystemBars(findViewById(R.id.root))
        address = findViewById(R.id.address)
        connect = findViewById(R.id.connect)
        status = findViewById(R.id.status)
        progress = findViewById(R.id.progress)
        if (savedInstanceState == null) intent.getStringExtra(EXTRA_PREVIOUS)?.let { address.setText(it) }
        connect.setOnClickListener { check() }
        address.setOnEditorActionListener { _, id, event ->
            if (id == EditorInfo.IME_ACTION_GO || event?.keyCode == KeyEvent.KEYCODE_ENTER) {
                check()
                true
            } else {
                false
            }
        }
    }

    private fun check() {
        val parsed = Hub.parseAddress(address.text.toString(), allowHttpDev = BuildConfig.DEBUG)
        if (parsed is Hub.Address.Bad) {
            status.text = parsed.reason
            return
        }
        val origin = (parsed as Hub.Address.Ok).origin
        busy(true)
        status.text = getString(R.string.setup_checking, origin)
        Thread {
            val result = HubClient(origin).check()
            runOnUiThread {
                if (isFinishing || isDestroyed) return@runOnUiThread
                busy(false)
                when (result) {
                    is HubClient.Check.Ok -> {
                        Store(this).origin = origin
                        startActivity(Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TASK or Intent.FLAG_ACTIVITY_NEW_TASK))
                        finish()
                    }
                    is HubClient.Check.Failed -> status.text = result.reason
                }
            }
        }.start()
    }

    private fun busy(on: Boolean) {
        connect.isEnabled = !on
        address.isEnabled = !on
        progress.visibility = if (on) View.VISIBLE else View.GONE
    }

    companion object {
        /** The address to start from when changing the hub. */
        const val EXTRA_PREVIOUS = "previous"
    }
}
