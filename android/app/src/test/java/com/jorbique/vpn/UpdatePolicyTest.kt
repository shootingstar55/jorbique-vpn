package com.jorbique.vpn

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UpdatePolicyTest {
    @Test fun requiresTheInstalledPackageAndSignerAndANewerVersion() {
        fun allowed(name: String? = "com.jorbique.vpn", version: Long = 10200,
                    signers: Set<String> = setOf("official")) = compatibleUpdate(
            "com.jorbique.vpn", 10101, setOf("official"), name, version, signers)
        assertTrue(allowed())
        assertFalse(allowed(name = "another.app"))
        assertFalse(allowed(name = null))
        assertFalse(allowed(version = 10101))
        assertFalse(allowed(version = 10000))
        assertFalse(allowed(signers = emptySet()))
        assertFalse(allowed(signers = setOf("attacker")))
        assertFalse(allowed(signers = setOf("official", "attacker")))
        assertFalse(compatibleUpdate("com.jorbique.vpn", 1, emptySet(),
            "com.jorbique.vpn", 2, emptySet()))
    }
}
