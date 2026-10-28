package com.jorbique.vpn

internal fun compatibleUpdate(
    expectedPackage: String, currentVersion: Long, installedSigners: Set<String>,
    packageName: String?, version: Long, signers: Set<String>,
): Boolean = packageName == expectedPackage && version > currentVersion &&
    installedSigners.isNotEmpty() && signers == installedSigners
