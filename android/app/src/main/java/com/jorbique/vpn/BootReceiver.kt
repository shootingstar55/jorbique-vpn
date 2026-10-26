package com.jorbique.vpn

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService
import com.jorbique.vpn.core.Core

/** Connects after the phone boots, when the autostart switch is on. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        if (!Settings.autostart(context) || !Core.hasUserUUID()) return
        // The VPN permission is given once, from the app; without it there is
        // nobody to ask at boot.
        if (VpnService.prepare(context) != null) return
        TunnelService.start(context)
    }
}
