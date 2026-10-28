package com.jorbique.vpn

import android.content.Intent
import android.graphics.Bitmap
import android.graphics.Canvas
import android.util.Base64
import android.webkit.JavascriptInterface
import android.webkit.WebView
import com.jorbique.vpn.core.Core
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.util.concurrent.ExecutionException
import java.util.concurrent.Executors

/**
 * What the page calls instead of the Wails bindings (frontend/src/lib/android.js).
 *
 * Each call runs on a background thread — Connect takes as long as the
 * connection check — and settles its promise in the page when done. Most
 * methods are the Go backend's (Core.call); the ones that need Android itself
 * are answered here.
 */
class Bridge(private val activity: MainActivity, private val webView: WebView) {

    private val executor = Executors.newCachedThreadPool()

    @JavascriptInterface
    fun methods(): String = JSONArray(GO_METHODS + ANDROID_METHODS).toString()

    @JavascriptInterface
    fun call(id: Int, method: String, args: String) {
        executor.execute {
            val (ok, payload) = try {
                true to handle(method, JSONArray(args.ifBlank { "[]" }))
            } catch (e: Throwable) {
                false to message(e)
            }
            webView.post {
                webView.evaluateJavascript(
                    "window.__jorbiqueSettle && window.__jorbiqueSettle($id, $ok, ${JSONObject.quote(payload)})",
                    null,
                )
            }
        }
    }

    @JavascriptInterface
    fun setTheme(dark: Boolean, background: String) = activity.applyTheme(dark, background)

    /** Returns the result as JSON ("" for none). */
    private fun handle(method: String, args: JSONArray): String = when (method) {
        "Connect" -> {
            if (!activity.ensureVpnPermission()) throw IllegalStateException("Нужно разрешить VPN-подключение")
            activity.askNotificationPermission()
            TunnelService.start(activity).get()
            ""
        }
        "InstallClientUpdate" -> {
            installClientUpdate(activity)
            ""
        }
        "Disconnect" -> {
            TunnelService.stop(activity)
            ""
        }
        // A phone is not started like a desktop session; the main screen hides the switch.
        "IsAutostartSupported" -> "false"
        "ListActiveApps" -> installedApps()
        "TakePendingImportUUID" -> JSONObject.quote(activity.takePendingImportUUID())
        else -> Core.call(method, args.toString())
    }

    /** The apps with a launcher icon, as rows of the exclusions list. */
    private fun installedApps(): String {
        val pm = activity.packageManager
        val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        @Suppress("DEPRECATION")
        val rows = pm.queryIntentActivities(launcher, 0)
            .map { it.activityInfo.applicationInfo }
            .distinctBy { it.packageName }
            .filter { it.packageName != activity.packageName }
            .map { info ->
                JSONObject().apply {
                    put("process_name", info.packageName)
                    put("exe_name", info.packageName)
                    put("window_title", pm.getApplicationLabel(info).toString())
                    runCatching { icon(pm.getApplicationIcon(info)) }.getOrNull()?.let { put("icon", it) }
                }
            }
            .sortedBy { it.getString("window_title").lowercase() }
        return JSONArray(rows).toString()
    }

    private fun icon(drawable: android.graphics.drawable.Drawable): String {
        val bitmap = Bitmap.createBitmap(ICON_PX, ICON_PX, Bitmap.Config.ARGB_8888)
        drawable.setBounds(0, 0, ICON_PX, ICON_PX)
        drawable.draw(Canvas(bitmap))
        val out = ByteArrayOutputStream()
        bitmap.compress(Bitmap.CompressFormat.PNG, 100, out)
        bitmap.recycle()
        return "data:image/png;base64," + Base64.encodeToString(out.toByteArray(), Base64.NO_WRAP)
    }

    companion object {
        private const val ICON_PX = 64

        private val GO_METHODS = listOf(
            "HasUserUUID", "HasVpnUserConfig", "GetUserUUID", "ActivateUserUUID", "SetUserUUID",
            "IsConnected", "CancelConnect", "GetConnectionProgress", "GetConnectionReport",
            "GetProfileProtocols", "GetAvailableOutbounds", "GetIPInfo",
            "GetDirectDomains", "SetDirectDomains",
            "GetIgnoredProcessNames", "GetIgnoredProcessPaths", "SetIgnoredProcessNamesAndPaths",
            "ListLogFiles", "GetLogContent", "ClearLogs",
            "GetAccount", "BindThisDevice", "UnbindDevice", "CheckClientUpdate", "GetClientUpdateProgress",
        )
        private val ANDROID_METHODS = listOf(
            "Connect", "Disconnect", "InstallClientUpdate", "IsAutostartSupported", "ListActiveApps", "TakePendingImportUUID",
        )

        private fun message(e: Throwable): String {
            val cause = if (e is ExecutionException) e.cause ?: e else e
            return cause.message?.takeIf { it.isNotBlank() } ?: cause.toString()
        }
    }
}
