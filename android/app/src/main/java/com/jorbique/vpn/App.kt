package com.jorbique.vpn

import android.app.Application
import android.app.NotificationChannel
import android.app.NotificationManager
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

