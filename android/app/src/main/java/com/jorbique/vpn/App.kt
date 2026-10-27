package com.jorbique.vpn

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
import android.os.Build
import android.provider.Settings
import com.jorbique.vpn.core.Core

class App : Application() {
    override fun onCreate() {
        super.onCreate()
        // The metrics name the phone as Settings does; Go alone sees "localhost".
        Core.setDeviceInfo(deviceName(), Build.VERSION.RELEASE ?: "", Build.ID ?: "")
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

    /** The name in Settings › About phone, or the maker and model. */
    private fun deviceName(): String {
        val named = runCatching { Settings.Global.getString(contentResolver, Settings.Global.DEVICE_NAME) }.getOrNull()
        if (!named.isNullOrBlank()) return named
        val model = Build.MODEL ?: ""
        val maker = Build.MANUFACTURER ?: ""
        return if (maker.isBlank() || model.startsWith(maker, ignoreCase = true)) model else "$maker $model"
    }

    companion object {
        const val NOTIFICATION_CHANNEL = "vpn"
    }
}

