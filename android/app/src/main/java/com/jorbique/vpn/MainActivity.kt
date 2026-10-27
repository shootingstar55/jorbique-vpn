package com.jorbique.vpn

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.webkit.WebViewAssetLoader
import java.util.concurrent.CompletableFuture
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/**
 * The window: the desktop client's Vue interface in a WebView, with Bridge
 * standing in for the Wails bindings.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var webView: WebView

    /** The UUID of a jorbique:// link the page has not taken yet. */
    @Volatile
    private var pendingImportUUID: String? = null

    private val vpnPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
            vpnPermission.getAndSet(null)?.complete(result.resultCode == RESULT_OK)
        }

    private val notificationPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        webView = WebView(this)
        // WebView draws its page under its own padding, so the system bars and
        // the camera cutout are kept clear by padding a frame around it instead.
        val frame = FrameLayout(this)
        frame.addView(webView, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))
        setContentView(frame)
        ViewCompat.setOnApplyWindowInsetsListener(frame) { view, insets ->
            val bars = insets.getInsets(
                WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout() or WindowInsetsCompat.Type.ime(),
            )
            view.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            WindowInsetsCompat.CONSUMED
        }

        val assets = WebViewAssetLoader.Builder()
            .addPathHandler("/assets/", WebViewAssetLoader.AssetsPathHandler(this))
            .build()
        webView.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? =
                assets.shouldInterceptRequest(request.url)

            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                if (request.url.host == APP_HOST) return false
                // Links out of the app open in the browser.
                runCatching { startActivity(Intent(Intent.ACTION_VIEW, request.url)) }
                return true
            }
        }
        webView.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            allowFileAccess = false
            allowContentAccess = false
            textZoom = 100
        }
        webView.setBackgroundColor(Color.TRANSPARENT)
        webView.addJavascriptInterface(Bridge(this, webView), "JorbiqueAndroid")
        webView.loadUrl("https://$APP_HOST/assets/web/index.html")
        // The page takes the link when it loads (TakePendingImportUUID).
        pendingImportUUID = importUUID(intent)
    }

    // singleTask: a link that finds the app open arrives here, and the page is
    // told to take it now.
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val uuid = importUUID(intent) ?: return
        pendingImportUUID = uuid
        webView.evaluateJavascript("window.dispatchEvent(new Event('jorbique-import'))", null)
    }

    /** Hands the page the waiting link's UUID once; "" when there is none. */
    fun takePendingImportUUID(): String {
        val uuid = pendingImportUUID ?: return ""
        pendingImportUUID = null
        return uuid
    }

    override fun onDestroy() {
        webView.destroy()
        super.onDestroy()
    }

    /**
     * Asks for the VPN permission when Android has not given it yet. Called on
     * a bridge thread; waits for the answer.
     */
    fun ensureVpnPermission(): Boolean {
        val intent = VpnService.prepare(this) ?: return true
        val answer = CompletableFuture<Boolean>()
        // A question still open from an earlier tap is answered "no": only one
        // dialog shows, and its answer settles this one.
        vpnPermission.getAndSet(answer)?.complete(false)
        runOnUiThread { vpnPermissionLauncher.launch(intent) }
        // Kept outside the activity: a window recreated while the dialog is up
        // (a theme or language change) gets the answer in its own launcher,
        // and this thread would otherwise wait forever.
        return runCatching { answer.get(PERMISSION_WAIT_MINUTES, TimeUnit.MINUTES) }.getOrDefault(false)
    }

    /** The connection notification needs this from Android 13 on; asked once, never required. */
    fun askNotificationPermission() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
        ) return
        runOnUiThread { notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS) }
    }

    /** Paints the window and the system bars in the page's theme. */
    fun applyTheme(dark: Boolean, background: String) {
        runOnUiThread {
            parseCssColor(background)?.let { window.decorView.setBackgroundColor(it) }
            WindowCompat.getInsetsController(window, window.decorView).apply {
                isAppearanceLightStatusBars = !dark
                isAppearanceLightNavigationBars = !dark
            }
        }
    }

    companion object {
        private const val PERMISSION_WAIT_MINUTES = 5L
        private val vpnPermission = AtomicReference<CompletableFuture<Boolean>?>()

        const val APP_HOST = "appassets.androidplatform.net"

        private val UUID_PATTERN =
            Regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

        /**
         * The UUID of a jorbique://import?uuid=… link, or null. Any page can
         * open such a link, so nothing but a well-formed UUID is read from it
         * (the same rule as internal/deeplink on the desktop).
         */
        fun importUUID(intent: Intent?): String? {
            if (intent?.action != Intent.ACTION_VIEW) return null
            val uri = intent.data ?: return null
            if (!uri.scheme.equals("jorbique", ignoreCase = true)) return null
            if (!uri.host.equals("import", ignoreCase = true)) return null
            val uuid = runCatching { uri.getQueryParameter("uuid") }.getOrNull()
                ?.trim()?.lowercase() ?: return null
            return uuid.takeIf { UUID_PATTERN.matches(it) }
        }

        private fun parseCssColor(value: String): Int? {
            val parts = Regex("""rgba?\(([^)]*)\)""").find(value)?.groupValues?.get(1)
                ?.split(',')?.map { it.trim() } ?: return null
            if (parts.size < 3) return null
            val (r, g, b) = parts.take(3).map { it.toFloatOrNull()?.toInt() ?: return null }
            return Color.rgb(r, g, b)
        }
    }
}
