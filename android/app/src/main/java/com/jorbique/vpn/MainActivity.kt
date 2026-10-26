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

/**
 * The window: the desktop client's Vue interface in a WebView, with Bridge
 * standing in for the Wails bindings.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var webView: WebView
    private var vpnPermission: CompletableFuture<Boolean>? = null

    private val vpnPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
            vpnPermission?.complete(result.resultCode == RESULT_OK)
            vpnPermission = null
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
        runOnUiThread {
            vpnPermission = answer
            vpnPermissionLauncher.launch(intent)
        }
        return answer.get()
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
        const val APP_HOST = "appassets.androidplatform.net"

        private fun parseCssColor(value: String): Int? {
            val parts = Regex("""rgba?\(([^)]*)\)""").find(value)?.groupValues?.get(1)
                ?.split(',')?.map { it.trim() } ?: return null
            if (parts.size < 3) return null
            val (r, g, b) = parts.take(3).map { it.toFloatOrNull()?.toInt() ?: return null }
            return Color.rgb(r, g, b)
        }
    }
}
