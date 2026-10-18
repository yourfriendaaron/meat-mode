package io.github.yourfriendaaron.meatmode

import android.os.Bundle
import android.widget.Button
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.activity.enableEdgeToEdge

/**
 * Debug builds only: the recent push events [PushLog] kept on this phone,
 * newest first, so delivery can be checked on the device itself.
 */
class PushLogActivity : ComponentActivity() {
    private lateinit var log: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_push_log)
        padForSystemBars(findViewById(R.id.root))
        log = findViewById(R.id.log)
        findViewById<Button>(R.id.refresh).setOnClickListener { show() }
        findViewById<Button>(R.id.clear).setOnClickListener {
            PushLog.clear(this)
            show()
        }
    }

    override fun onResume() {
        super.onResume()
        show()
    }

    private fun show() {
        val lines = PushLog.read(this)
        log.text = if (lines.isEmpty()) getString(R.string.push_log_empty) else lines.joinToString("\n")
    }
}
