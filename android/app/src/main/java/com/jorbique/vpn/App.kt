package com.jorbique.vpn

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import com.jorbique.vpn.core.Core

class App : Application() {
    override fun onCreate() {
        super.onCreate()
        // The activity and the VPN service share this process and the Go
        // engine in it; whichever starts first sets it up.
        Core.setup(filesDir.path, cacheDir.path, true)
        getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel(
                NOTIFICATION_CHANNEL,
                getString(R.string.notification_channel),
                NotificationManager.IMPORTANCE_LOW,
            ).apply { setShowBadge(false) },
        )
    }

    companion object {
        const val NOTIFICATION_CHANNEL = "vpn"
    }
}

/** The switch on the main screen: connect when the phone has booted. */
object Settings {
    private const val PREFS = "settings"
    private const val AUTOSTART = "autostart"

    fun autostart(context: Context): Boolean =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(AUTOSTART, false)

    fun setAutostart(context: Context, on: Boolean) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(AUTOSTART, on).apply()
    }
}
