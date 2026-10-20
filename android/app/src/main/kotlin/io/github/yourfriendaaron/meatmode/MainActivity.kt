package io.github.yourfriendaaron.meatmode

import android.Manifest
import android.app.AlertDialog
import android.app.DownloadManager
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.Uri
import android.net.http.SslError
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.provider.Settings
import android.view.View
import android.webkit.CookieManager
import android.webkit.RenderProcessGoneDetail
import android.webkit.SslErrorHandler
import android.webkit.URLUtil
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.addCallback
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import androidx.core.net.toUri
import androidx.core.view.WindowInsetsControllerCompat
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import org.json.JSONObject
import java.io.File
import java.io.IOException
import org.unifiedpush.android.connector.UnifiedPush
import org.unifiedpush.android.connector.UnifiedPush.VapidNotValidException
import org.unifiedpush.android.connector.data.ResolvedDistributor

/**
 * The hub's own pages in a WebView. Native code covers only what a page in a
 * WebView cannot do: push, notifications and their buttons, and the camera
 * for Take a photo (docs/api.md,
 * Android app).
 */
class MainActivity : ComponentActivity() {
    private lateinit var store: Store
    private lateinit var origin: String
    private lateinit var root: View
    private lateinit var web: WebView
    private lateinit var offline: View
    private lateinit var offlineMessage: TextView

    private var fileCallback: ValueCallback<Array<Uri>>? = null
    private val pickFiles = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { r ->
        fileCallback?.onReceiveValue(WebChromeClient.FileChooserParams.parseResult(r.resultCode, r.data))
        fileCallback = null
    }

    // Take a photo: the camera app saves into photoFile, and the page gets its
    // URI. The result intent may be empty, so the file itself is the answer.
    private var photoFile: File? = null
    private var photoUri: Uri? = null
    private val takePhoto = registerForActivityResult(ActivityResultContracts.TakePicture()) { saved ->
        val file = photoFile
        val uri = photoUri
        photoFile = null
        photoUri = null
        val taken = saved && uri != null && file != null && file.length() > 0
        fileCallback?.onReceiveValue(if (taken) arrayOf(uri!!) else null)
        fileCallback = null
    }

    private var permissionWaiter: ((Boolean) -> Unit)? = null
    private val askNotifications = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        permissionWaiter?.invoke(granted)
        permissionWaiter = null
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        store = Store(this)
        val saved = store.origin
        if (saved == null) {
            startActivity(Intent(this, SetupActivity::class.java))
            finish()
            return
        }
        origin = saved
        setContentView(R.layout.activity_main)
        root = findViewById(R.id.root)
        web = findViewById(R.id.web)
        offline = findViewById(R.id.offline)
        offlineMessage = findViewById(R.id.offline_message)
        padForSystemBars(root)
        Notifier.ensureChannels(this)

        findViewById<Button>(R.id.retry).setOnClickListener {
            showPage()
            web.loadUrl(web.url ?: Hub.inAppUrl(null, origin))
        }
        findViewById<Button>(R.id.open_settings).setOnClickListener {
            startActivity(Intent(this, SettingsActivity::class.java))
        }

        setUpWebView()
        onBackPressedDispatcher.addCallback(this) {
            if (web.canGoBack()) {
                web.goBack()
            } else {
                isEnabled = false
                onBackPressedDispatcher.onBackPressed()
                isEnabled = true
            }
        }

        if (savedInstanceState == null || web.restoreState(savedInstanceState) == null) {
            web.loadUrl(Hub.inAppUrl(pathFrom(intent), origin))
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        val path = pathFrom(intent) ?: return
        showPage()
        web.loadUrl(Hub.inAppUrl(path, origin))
    }

    override fun onStart() {
        super.onStart()
        if (::web.isInitialized) Registration.refresh(this)
    }

    override fun onPause() {
        super.onPause()
        CookieManager.getInstance().flush()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        if (::web.isInitialized) web.saveState(outState)
    }

    private fun pathFrom(intent: Intent?): String? =
        if (intent?.action == ACTION_OPEN) intent.getStringExtra(EXTRA_PATH) else null

    // The hub's UI is a JavaScript app; the bridge checks every message it gets.
    @android.annotation.SuppressLint("SetJavaScriptEnabled")
    private fun setUpWebView() {
        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            allowFileAccess = false
            allowContentAccess = false
            mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
            setSupportMultipleWindows(false)
        }
        CookieManager.getInstance().apply {
            setAcceptCookie(true)
            setAcceptThirdPartyCookies(web, false)
        }

        // The bridge exists for the saved hub origin only, never for other pages.
        if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            val bridge = Bridge(BuildConfig.VERSION_NAME, AppPush())
            try {
                WebViewCompat.addWebMessageListener(web, Bridge.JS_NAME, setOf(origin)) { _, message, source, isMainFrame, reply ->
                    if (!isMainFrame || Hub.originOf(source.toString()) != origin) return@addWebMessageListener
                    val data = message.data ?: return@addWebMessageListener
                    bridge.handle(data) { out ->
                        runOnUiThread {
                            // The page may have gone since it asked; its answer goes nowhere then.
                            try {
                                reply.postMessage(out)
                            } catch (e: IllegalStateException) {
                                PushLog.add(this, "bridge answer dropped: ${e.message}")
                            }
                        }
                    }
                }
            } catch (e: IllegalArgumentException) {
                PushLog.add(this, "bridge not set up: ${e.message}")
            }
        }

        web.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val url = request.url.toString()
                if (Hub.isSameOrigin(url, origin)) return false
                openOutside(request.url)
                return true
            }

            override fun onPageFinished(view: WebView, url: String?) {
                CookieManager.getInstance().flush()
                matchPageColor()
            }

            override fun doUpdateVisitedHistory(view: WebView, url: String?, isReload: Boolean) {
                matchPageColor()
            }

            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (request.isForMainFrame) showOffline(getString(R.string.offline_unreachable))
            }

            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                // The WebView is unusable once its renderer is gone; start the screen again.
                PushLog.add(this@MainActivity, "WebView renderer gone, crashed: ${detail.didCrash()}")
                recreate()
                return true
            }

            override fun onReceivedSslError(view: WebView, handler: SslErrorHandler, error: SslError) {
                handler.cancel()
                showOffline(getString(R.string.offline_certificate))
            }
        }

        web.webChromeClient = object : WebChromeClient() {
            override fun onShowFileChooser(
                view: WebView,
                callback: ValueCallback<Array<Uri>>,
                params: FileChooserParams,
            ): Boolean {
                // Each callback must be answered, a cancelled pick included.
                fileCallback?.onReceiveValue(null)
                fileCallback = callback
                // capture opens the camera; with no camera app it falls back to the picker.
                if (params.isCaptureEnabled && Capture.acceptsImages(params.acceptTypes) && launchCamera()) {
                    return true
                }
                try {
                    pickFiles.launch(params.createIntent())
                } catch (_: ActivityNotFoundException) {
                    fileCallback = null
                    callback.onReceiveValue(null)
                }
                return true
            }
        }

        web.setDownloadListener { url, userAgent, disposition, mimeType, _ ->
            if (Hub.isSameOrigin(url, origin)) download(url, userAgent, disposition, mimeType) else openOutside(url.toUri())
        }
    }

    /**
     * Saves a hub file, such as the export or an attachment, to Downloads.
     * The hub's files need the session cookie, which only the WebView holds,
     * so the download carries it; the default browser would get "log in
     * first". Before Android 10 saving to Downloads needs a storage
     * permission the app does not ask for, so there the browser gets the link.
     */
    private fun download(url: String, userAgent: String, disposition: String?, mimeType: String?) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            openOutside(url.toUri())
            return
        }
        val name = URLUtil.guessFileName(url, disposition, mimeType)
        val request = DownloadManager.Request(url.toUri())
            .setTitle(name)
            .setMimeType(mimeType)
            .addRequestHeader("User-Agent", userAgent)
            .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
            .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, name)
        CookieManager.getInstance().getCookie(url)?.let { request.addRequestHeader("Cookie", it) }
        try {
            getSystemService(DownloadManager::class.java).enqueue(request)
            Toast.makeText(this, getString(R.string.download_started, name), Toast.LENGTH_SHORT).show()
        } catch (e: IllegalArgumentException) {
            PushLog.add(this, "download refused: ${e.message}")
            openOutside(url.toUri())
        } catch (e: IllegalStateException) {
            PushLog.add(this, "download refused: ${e.message}")
            openOutside(url.toUri())
        }
    }

    /**
     * Starts the camera app on a new file in cacheDir/photos, after deleting
     * the earlier captures there. False when no camera app answers or the
     * file cannot be made, so the caller falls back to the picker.
     */
    private fun launchCamera(): Boolean {
        val dir = File(cacheDir, "photos")
        val file = File(dir, "photo-${System.currentTimeMillis()}.jpg")
        val uri = try {
            dir.listFiles()?.forEach { it.delete() }
            if (!dir.isDirectory && !dir.mkdirs()) return false
            FileProvider.getUriForFile(this, "$packageName.photos", file)
        } catch (_: IOException) {
            return false
        } catch (_: IllegalArgumentException) {
            return false
        }
        photoFile = file
        photoUri = uri
        return try {
            takePhoto.launch(uri)
            true
        } catch (_: ActivityNotFoundException) {
            photoFile = null
            photoUri = null
            false
        }
    }

    /** Links off the hub open in the default browser, or the app that handles them. */
    private fun openOutside(uri: Uri) {
        if (uri.scheme?.lowercase() !in setOf("https", "http", "mailto", "tel", "webcal")) return
        try {
            startActivity(Intent(Intent.ACTION_VIEW, uri).addCategory(Intent.CATEGORY_BROWSABLE))
        } catch (_: ActivityNotFoundException) {
            // Nothing on the phone opens it.
        }
    }

    private fun showOffline(message: String) {
        offlineMessage.text = message
        offline.visibility = View.VISIBLE
        web.visibility = View.INVISIBLE
    }

    private fun showPage() {
        offline.visibility = View.GONE
        web.visibility = View.VISIBLE
    }

    /** Gives the bars behind the system icons the page's theme colour, and icons that read on it. */
    private fun matchPageColor() {
        web.evaluateJavascript("(function(){var m=document.querySelector('meta[name=\"theme-color\"]');return m?m.content:''})()") { result ->
            // A page that has not parsed its head yet has no colour; the next callback brings it.
            val color = PageColor.parse(result) ?: return@evaluateJavascript
            root.setBackgroundColor(color)
            val light = (Color.red(color) * 299 + Color.green(color) * 587 + Color.blue(color) * 114) / 1000 > 140
            WindowInsetsControllerCompat(window, root).apply {
                isAppearanceLightStatusBars = light
                isAppearanceLightNavigationBars = light
            }
        }
    }

    /** The bridge's push verbs, run on this activity for its dialogs and permission prompt. */
    private inner class AppPush : PushControl {
        override fun status(): JSONObject = Registration.status(this@MainActivity)

        override fun pushOn(vapidKey: String, done: (Result<JSONObject>) -> Unit) {
            val fail = { message: String -> done(Result.failure(Registration.Failure(message))) }
            askNotificationPermission { granted ->
                if (!granted) {
                    fail(getString(R.string.push_blocked))
                    return@askNotificationPermission
                }
                pickDistributor { problem ->
                    if (problem != null) {
                        fail(problem)
                        return@pickDistributor
                    }
                    register(vapidKey, done)
                }
            }
        }

        override fun pushOff(done: (Result<JSONObject>) -> Unit) {
            Registration.forget(this@MainActivity, revoke = true)
            done(Result.success(JSONObject().put("state", PushState.OFF)))
        }

        private fun register(vapidKey: String, done: (Result<JSONObject>) -> Unit) {
            store.vapidKey = vapidKey
            store.registeredAt = System.currentTimeMillis()
            Registration.await { result ->
                done(result)
                if (result.isSuccess) askBatteryOnce()
            }
            try {
                UnifiedPush.register(this@MainActivity, messageForDistributor = getString(R.string.app_name), vapid = vapidKey)
            } catch (_: VapidNotValidException) {
                Registration.finish(Result.failure(Registration.Failure("The hub's VAPID key is not valid.")))
            }
        }
    }

    private fun askNotificationPermission(then: (Boolean) -> Unit) {
        if (Build.VERSION.SDK_INT < 33 ||
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED
        ) {
            then(NotificationManagerCompat.from(this).areNotificationsEnabled())
            return
        }
        permissionWaiter = then
        askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
    }

    /** Saves a distributor, asking first when Android will show its picker. Null when one is saved. */
    private fun pickDistributor(then: (String?) -> Unit) {
        if (UnifiedPush.getAckDistributor(this) != null) {
            then(null)
            return
        }
        when (val r = UnifiedPush.resolveDefaultDistributor(this)) {
            is ResolvedDistributor.Found -> {
                UnifiedPush.saveDistributor(this, r.packageName)
                then(null)
            }
            ResolvedDistributor.ToSelect -> {
                var answered = false
                val answer = { problem: String? ->
                    if (!answered) {
                        answered = true
                        then(problem)
                    }
                }
                AlertDialog.Builder(this)
                    .setTitle(R.string.pick_title)
                    .setMessage(R.string.pick_message)
                    .setPositiveButton(R.string.pick_continue) { _, _ ->
                        UnifiedPush.tryUseDefaultDistributor(this) { ok ->
                            runOnUiThread { answer(if (ok) null else getString(R.string.pick_none_chosen)) }
                        }
                    }
                    .setNegativeButton(android.R.string.cancel) { _, _ -> answer(getString(R.string.pick_none_chosen)) }
                    .setOnCancelListener { answer(getString(R.string.pick_none_chosen)) }
                    .show()
            }
            ResolvedDistributor.NoneAvailable -> then(getString(R.string.push_unavailable))
        }
    }

    /** With a distributor app, asks once to let Meat Mode run unrestricted, so pushes can wake it. */
    private fun askBatteryOnce() {
        if (store.batteryAsked || !Registration.usesDistributorApp(this) || Registration.batteryExempt(this)) return
        store.batteryAsked = true
        AlertDialog.Builder(this)
            .setTitle(R.string.battery_title)
            .setMessage(R.string.battery_message)
            .setPositiveButton(R.string.battery_allow) { _, _ -> requestBatteryExemption(this) }
            .setNegativeButton(R.string.battery_not_now, null)
            .show()
    }

    companion object {
        const val ACTION_OPEN = "io.github.yourfriendaaron.meatmode.OPEN"
        const val EXTRA_PATH = "path"
    }
}

/** Android's own prompt to let the app ignore battery optimisation. */
@android.annotation.SuppressLint("BatteryLife")
fun requestBatteryExemption(activity: android.app.Activity) {
    // The app is not on Google Play; reminders through a distributor app need the exemption.
    try {
        activity.startActivity(
            Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, ("package:" + activity.packageName).toUri()),
        )
    } catch (_: ActivityNotFoundException) {
        activity.startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
    }
}
