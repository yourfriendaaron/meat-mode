plugins {
    alias(libs.plugins.android.application)
}

// The version comes from -PappVersion=X.Y.Z (CI passes the tag without its v);
// versionCode is X*1000000 + Y*1000 + Z (docs/api.md, Android app). Android
// refuses a versionCode of 0, so the default 0.0.0 gets 1.
val appVersion = (findProperty("appVersion") as String?)?.trim().orEmpty().ifEmpty { "0.0.0" }
val appVersionCode: Int = run {
    val m = Regex("""^(\d{1,3})\.(\d{1,3})\.(\d{1,3})$""").matchEntire(appVersion)
        ?: throw GradleException("appVersion must be X.Y.Z with each part below 1000, got \"$appVersion\"")
    val (x, y, z) = m.destructured
    maxOf(1, x.toInt() * 1_000_000 + y.toInt() * 1_000 + z.toInt())
}

// Release signing comes from the environment only. Any one unset fails the
// release build, so no unsigned or debug-signed release APK can exist.
val signingVars = listOf(
    "MEAT_MODE_ANDROID_KEYSTORE",
    "MEAT_MODE_ANDROID_KEYSTORE_PASSWORD",
    "MEAT_MODE_ANDROID_KEY_ALIAS",
    "MEAT_MODE_ANDROID_KEY_PASSWORD",
)
val signingEnv = signingVars.associateWith { System.getenv(it).orEmpty() }
val missingSigning = signingVars.filter { signingEnv.getValue(it).isEmpty() }

android {
    namespace = "io.github.yourfriendaaron.meatmode"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.yourfriendaaron.meatmode"
        minSdk = 26
        targetSdk = 36
        versionCode = appVersionCode
        versionName = appVersion
    }

    signingConfigs {
        create("release") {
            if (missingSigning.isEmpty()) {
                storeFile = file(signingEnv.getValue("MEAT_MODE_ANDROID_KEYSTORE"))
                storePassword = signingEnv.getValue("MEAT_MODE_ANDROID_KEYSTORE_PASSWORD")
                keyAlias = signingEnv.getValue("MEAT_MODE_ANDROID_KEY_ALIAS")
                keyPassword = signingEnv.getValue("MEAT_MODE_ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            // No shrinking: R8 problems only show on a device, and the APK is small either way.
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName("release")
        }
    }

    buildFeatures {
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    lint {
        warningsAsErrors = false
        abortOnError = true
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

// preReleaseBuild is the first task of every release build: fail there, naming
// what is missing, before anything is compiled or packaged.
tasks.matching { it.name == "preReleaseBuild" }.configureEach {
    val missing = missingSigning
    doFirst {
        if (missing.isNotEmpty()) {
            throw GradleException("A release build needs ${missing.joinToString(", ")} set")
        }
    }
}

dependencies {
    implementation(libs.androidx.activity)
    implementation(libs.androidx.core)
    implementation(libs.androidx.webkit)
    implementation(libs.androidx.work)
    implementation(libs.unifiedpush.connector)
    implementation(libs.unifiedpush.fcm)

    testImplementation(libs.junit)
    // Android's own org.json is a stub on the JVM; tests use the real one.
    testImplementation(libs.json)
}
