package com.jorbique.vpn

import android.app.Notification
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.DnsResolver
import android.net.IpPrefix
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.Build
import android.os.CancellationSignal
import android.os.Handler
import android.os.HandlerThread
import android.os.ParcelFileDescriptor
import com.jorbique.vpn.core.Core
import com.jorbique.vpn.core.Platform
import org.json.JSONArray
import org.json.JSONObject
import java.net.InetAddress
import java.util.concurrent.CompletableFuture
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/**
 * The VPN. It hosts the Go engine's tunnel: the engine checks the protocols
 * and starts sing-box, and asks this service for the interface, for sockets
 * kept out of it and for the state of the phone's network (Platform).
 */
class TunnelService : VpnService(), Platform {

    private val connectivity by lazy { getSystemService(ConnectivityManager::class.java) }
    private val callbackThread = HandlerThread("network-monitor").apply { start() }
    private val callbackHandler = Handler(callbackThread.looper)

    @Volatile private var tun: ParcelFileDescriptor? = null
    @Volatile private var underlying: Network? = null
    @Volatile private var running = false
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private var screenReceiver: BroadcastReceiver? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                worker.execute {
                    Core.disconnect()
                    shutDown()
                }
                return START_NOT_STICKY
            }
            else -> connect()
        }
        return START_NOT_STICKY
    }

    private fun connect() {
        if (running) {
            // Already up: answer the new request. Still connecting: the
            // running attempt settles it.
            if (Core.isConnected()) pendingConnect.getAndSet(null)?.complete(null)
            return
        }
        showNotification(getString(R.string.notification_connecting))
        running = true
        Core.setPlatform(this)
        registerScreenReceiver()
        worker.execute {
            try {
                Core.connect()
                showNotification(getString(R.string.notification_connected))
                pendingConnect.getAndSet(null)?.complete(null)
            } catch (e: Exception) {
                pendingConnect.getAndSet(null)?.completeExceptionally(e)
                shutDown()
            }
        }
    }

    /** Ends the service once the engine has stopped. */
    private fun shutDown() {
        running = false
        closeTun()
        unregisterScreenReceiver()
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onRevoke() {
        // Another VPN took over, or the user turned this one off in settings.
        worker.execute {
            Core.disconnect()
            shutDown()
        }
    }

    override fun onDestroy() {
        if (running) Core.disconnect()
        closeTun()
        stopNetworkMonitor()
        unregisterScreenReceiver()
        Core.setPlatform(null)
        callbackThread.quitSafely()
        super.onDestroy()
    }

    private fun closeTun() {
        tun?.let { runCatching { it.close() } }
        tun = null
    }

    private fun showNotification(text: String) {
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val stop = PendingIntent.getService(
            this, 1, Intent(this, TunnelService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val notification = Notification.Builder(this, App.NOTIFICATION_CHANNEL)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(text)
            .setContentIntent(open)
            .setOngoing(true)
            .addAction(Notification.Action.Builder(null, getString(R.string.notification_disconnect), stop).build())
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
    }

    private fun registerScreenReceiver() {
        if (screenReceiver != null) return
        val receiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                when (intent.action) {
                    Intent.ACTION_SCREEN_OFF -> worker.execute { Core.pause() }
                    Intent.ACTION_SCREEN_ON -> worker.execute { Core.wake() }
                }
            }
        }
        val filter = IntentFilter().apply {
            addAction(Intent.ACTION_SCREEN_ON)
            addAction(Intent.ACTION_SCREEN_OFF)
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(receiver, filter, RECEIVER_NOT_EXPORTED)
        } else {
            registerReceiver(receiver, filter)
        }
        screenReceiver = receiver
    }

    private fun unregisterScreenReceiver() {
        screenReceiver?.let { runCatching { unregisterReceiver(it) } }
        screenReceiver = null
    }

    // --- Platform: called by the engine -----------------------------------------------

    override fun openTun(request: String): Int {
        if (prepare(this) != null) throw IllegalStateException("Нет разрешения на VPN")
        val options = JSONObject(request)
        val builder = Builder()
            .setSession(getString(R.string.app_name))
            .setMtu(options.optInt("mtu", 9000))
            .setMetered(false)
        forEach(options, "addresses") { builder.addAddress(it.first, it.second) }
        forEach(options, "routes") { builder.addRoute(it.first, it.second) }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            forEach(options, "excludeRoutes") { builder.excludeRoute(IpPrefix(it.first, it.second)) }
        }
        strings(options, "dnsServers").forEach { builder.addDnsServer(it) }
        val include = strings(options, "includePackage")
        if (include.isNotEmpty()) {
            include.forEach { name -> ignoreMissing { builder.addAllowedApplication(name) } }
        } else {
            strings(options, "excludePackage").forEach { name -> ignoreMissing { builder.addDisallowedApplication(name) } }
        }
        underlying?.let { builder.setUnderlyingNetworks(arrayOf(it)) }
        builder.setConfigureIntent(
            PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE),
        )
        val fd = builder.establish() ?: throw IllegalStateException("Android не дал поднять VPN")
        closeTun()
        tun = fd
        return fd.fd
    }

    override fun protectSocket(fd: Int) {
        if (!protect(fd)) throw IllegalStateException("protect failed")
    }

    override fun startNetworkMonitor() {
        if (networkCallback != null) return
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) = report(network)
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) = report(network)
            override fun onLinkPropertiesChanged(network: Network, lp: LinkProperties) = report(network)
            override fun onLost(network: Network) {
                if (network == underlying) {
                    underlying = null
                    setUnderlyingNetworks(null)
                    Core.updateDefaultInterface("", -1, false, false)
                }
            }
        }
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_RESTRICTED)
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            connectivity.registerBestMatchingNetworkCallback(request, callback, callbackHandler)
        } else {
            connectivity.requestNetwork(request, callback, callbackHandler)
        }
        networkCallback = callback
        // The callback reports asynchronously; the engine starts dialling now.
        physicalActiveNetwork()?.let { report(it) }
    }

    /** The active network when it is not a VPN — before the tunnel, the phone's own. */
    private fun physicalActiveNetwork(): Network? {
        val network = connectivity.activeNetwork ?: return null
        val caps = connectivity.getNetworkCapabilities(network) ?: return null
        return network.takeUnless { caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN) }
    }

    override fun stopNetworkMonitor() {
        networkCallback?.let { runCatching { connectivity.unregisterNetworkCallback(it) } }
        networkCallback = null
    }

    private fun report(network: Network) {
        val lp = connectivity.getLinkProperties(network) ?: return
        val name = lp.interfaceName ?: return
        val index = interfaceIndex(name)
        if (index < 0) return
        val caps = connectivity.getNetworkCapabilities(network)
        val expensive = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED) != true
        val changed = underlying != network
        underlying = network
        if (changed) {
            setUnderlyingNetworks(arrayOf(network))
            worker.execute { Core.resetNetwork() }
        }
        Core.updateDefaultInterface(name, index, expensive, false)
    }

    override fun networkInterfaces(): String {
        val out = JSONArray()
        @Suppress("DEPRECATION")
        for (network in connectivity.allNetworks) {
            val caps = connectivity.getNetworkCapabilities(network) ?: continue
            if (caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) continue
            val lp = connectivity.getLinkProperties(network) ?: continue
            val name = lp.interfaceName ?: continue
            val nic = runCatching { java.net.NetworkInterface.getByName(name) }.getOrNull() ?: continue
            var flags = 0
            if (nic.isUp) flags = flags or IFF_UP or IFF_RUNNING
            if (nic.isLoopback) flags = flags or IFF_LOOPBACK
            if (nic.isPointToPoint) flags = flags or IFF_POINTOPOINT
            if (nic.supportsMulticast()) flags = flags or IFF_MULTICAST
            val type = when {
                caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> 0
                caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> 1
                caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> 2
                else -> 3
            }
            out.put(JSONObject().apply {
                put("index", nic.index)
                put("mtu", runCatching { nic.mtu }.getOrDefault(1500))
                put("name", name)
                put("addresses", JSONArray(lp.linkAddresses.map { "${host(it.address)}/${it.prefixLength}" }))
                put("flags", flags)
                put("type", type)
                put("dns", JSONArray(lp.dnsServers.map { host(it) }))
                put("gateways", JSONArray(lp.routes.filter { it.isDefaultRoute && it.gateway != null }.map { host(it.gateway!!) }))
                put("metered", !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED))
            })
        }
        return out.toString()
    }

    override fun queryDNS(query: ByteArray): ByteArray {
        // Never the default network: once the tunnel is up that is the tunnel,
        // and the engine would be asking itself.
        val network = underlying ?: physicalActiveNetwork() ?: throw IllegalStateException("no network")
        val answer = AtomicReference<ByteArray?>()
        val failure = AtomicReference<Throwable?>()
        val done = CountDownLatch(1)
        val cancel = CancellationSignal()
        DnsResolver.getInstance().rawQuery(
            network, query, DnsResolver.FLAG_NO_RETRY, dnsExecutor, cancel,
            object : DnsResolver.Callback<ByteArray> {
                override fun onAnswer(result: ByteArray, rcode: Int) {
                    answer.set(result)
                    done.countDown()
                }

                override fun onError(error: DnsResolver.DnsException) {
                    failure.set(error)
                    done.countDown()
                }
            },
        )
        if (!done.await(DNS_TIMEOUT_SECONDS, TimeUnit.SECONDS)) {
            cancel.cancel()
            throw IllegalStateException("DNS timeout")
        }
        failure.get()?.let { throw it }
        return answer.get() ?: throw IllegalStateException("empty DNS answer")
    }

    companion object {
        const val ACTION_STOP = "com.jorbique.vpn.STOP"
        private const val NOTIFICATION_ID = 1
        private const val DNS_TIMEOUT_SECONDS = 10L
        private const val IFF_UP = 0x1
        private const val IFF_LOOPBACK = 0x8
        private const val IFF_POINTOPOINT = 0x10
        private const val IFF_RUNNING = 0x40
        private const val IFF_MULTICAST = 0x1000

        private val worker = Executors.newCachedThreadPool()
        private val dnsExecutor = Executors.newCachedThreadPool()
        private val pendingConnect = AtomicReference<CompletableFuture<Void?>?>()

        /** Starts the service and connects; the future settles when the tunnel is up or failed. */
        fun start(context: Context): CompletableFuture<Void?> {
            val future = CompletableFuture<Void?>()
            pendingConnect.getAndSet(future)?.cancel(false)
            context.startForegroundService(Intent(context, TunnelService::class.java))
            return future
        }

        /** Disconnects and stops the service. Blocks until the engine has stopped. */
        fun stop(context: Context) {
            Core.disconnect()
            context.startService(Intent(context, TunnelService::class.java).setAction(ACTION_STOP))
        }

        private fun interfaceIndex(name: String): Int =
            runCatching { java.net.NetworkInterface.getByName(name)?.index }.getOrNull() ?: -1

        private fun host(address: InetAddress): String = address.hostAddress?.substringBefore('%') ?: ""

        private fun strings(options: JSONObject, key: String): List<String> {
            val array = options.optJSONArray(key) ?: return emptyList()
            return (0 until array.length()).map { array.getString(it) }
        }

        private fun forEach(options: JSONObject, key: String, action: (Pair<InetAddress, Int>) -> Unit) {
            for (prefix in strings(options, key)) {
                val address = InetAddress.getByName(prefix.substringBefore('/'))
                val length = prefix.substringAfter('/', "").toIntOrNull()
                    ?: if (address.address.size == 4) 32 else 128
                action(address to length)
            }
        }

        private inline fun ignoreMissing(block: () -> Unit) {
            try {
                block()
            } catch (_: PackageManager.NameNotFoundException) {
                // An excluded app that has since been uninstalled.
            }
        }
    }
}
