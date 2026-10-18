package io.github.yourfriendaaron.meatmode

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The build sets versionCode from -PappVersion=X.Y.Z as X*1000000 + Y*1000 + Z
 * (app/build.gradle.kts); this checks the build this test runs in agrees.
 */
class VersionTest {
    @Test
    fun versionCodeFollowsVersionName() {
        val (x, y, z) = BuildConfig.VERSION_NAME.split(".").map { it.toInt() }
        assertEquals(maxOf(1, x * 1_000_000 + y * 1_000 + z), BuildConfig.VERSION_CODE)
    }
}
