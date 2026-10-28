package com.jorbique.vpn

import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.Settings
import androidx.core.content.FileProvider
import com.jorbique.vpn.core.Core
import org.json.JSONTokener
import java.io.File
import java.util.concurrent.CompletableFuture
import java.util.concurrent.TimeUnit

/** A verified APK still passes Android's package identity and signature checks. */
@Suppress("DEPRECATION")
internal fun installClientUpdate(activity: MainActivity) {
    if (!activity.packageManager.canRequestPackageInstalls()) {
        launchInstaller(activity, Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
            Uri.parse("package:${activity.packageName}")))
        error("Разрешите установку обновлений для Jorbique VPN и нажмите «Обновить» ещё раз")
    }
    val path = JSONTokener(Core.call("DownloadClientUpdate", "[]")).nextValue() as? String
        ?: error("Установщик не получен")
    val file = File(path).canonicalFile
    val root = File(activity.cacheDir, "jorbique-client-updates").canonicalFile
    check(file.toPath().startsWith(root.toPath()) && file.isFile) { "Недопустимый файл обновления" }
    val pm = activity.packageManager
    val candidate = pm.getPackageArchiveInfo(file.path, PackageManager.GET_SIGNING_CERTIFICATES)
        ?: error("Некорректный APK")
    val installed = pm.getPackageInfo(activity.packageName, PackageManager.GET_SIGNING_CERTIFICATES)
    val signers = candidate.signingInfo?.apkContentsSigners?.map { it.toCharsString() }?.toSet().orEmpty()
    val installedSigners = installed.signingInfo?.apkContentsSigners?.map { it.toCharsString() }?.toSet().orEmpty()
    check(compatibleUpdate(activity.packageName, installed.longVersionCode, installedSigners,
        candidate.packageName, candidate.longVersionCode, signers)) {
        "Имя пакета, версия или подпись обновления не совпадают с установленным приложением"
    }
    val uri = FileProvider.getUriForFile(activity, "${activity.packageName}.updates", file)
    launchInstaller(activity, Intent(Intent.ACTION_VIEW).apply {
        setDataAndType(uri, "application/vnd.android.package-archive")
        addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    })
}

private fun launchInstaller(activity: MainActivity, intent: Intent) {
    val result = CompletableFuture<Unit>()
    activity.runOnUiThread {
        runCatching {
            check(!activity.isDestroyed && !activity.isFinishing) { "Откройте приложение для установки обновления" }
            activity.startActivity(intent)
        }.fold({ result.complete(Unit) }, { result.completeExceptionally(it) })
    }
    result.get(15, TimeUnit.SECONDS)
}
