plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// The release number is the repository's VERSION, the same one the desktop
// builds read; versionCode is derived from it so every release installs over
// the last.
val appVersion = rootProject.file("../VERSION").readText().trim()
val appVersionCode = appVersion.split("-")[0].split(".").map { it.toInt() }
    .let { (major, minor, patch) -> major * 10000 + minor * 100 + patch }

// Release signing comes from the environment (CI fills it from Actions
// secrets). Without it the release is signed with the debug key, which
// installs but cannot be updated by a properly signed build later.
val keystoreFile = System.getenv("ANDROID_KEYSTORE_FILE")?.takeIf { it.isNotBlank() }?.let { file(it) }

android {
    namespace = "com.jorbique.vpn"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.jorbique.vpn"
        minSdk = 29
        targetSdk = 35
        versionCode = appVersionCode
        versionName = appVersion
        ndk {
            abiFilters += "arm64-v8a"
        }
    }

    signingConfigs {
        if (keystoreFile != null && keystoreFile.exists()) {
            create("release") {
                storeFile = keystoreFile
                storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ANDROID_KEY_ALIAS")
                keyPassword = System.getenv("ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        buildConfig = true
    }

    lint {
        // Lint's release pass is not a gate for this build.
        checkReleaseBuilds = false
    }

    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }

    sourceSets["main"].assets.srcDir(layout.buildDirectory.dir("generated/webui"))
}

// The window is the desktop client's Vue interface: frontend/dist, built by
// `npm run build` in frontend/, served from assets/web.
val copyWebUi by tasks.registering(Copy::class) {
    from(rootProject.file("../frontend/dist"))
    into(layout.buildDirectory.dir("generated/webui/web"))
    doFirst {
        check(rootProject.file("../frontend/dist/index.html").exists()) {
            "frontend/dist is missing: run `npm ci && npm run build` in frontend/ first"
        }
    }
}
tasks.named("preBuild") { dependsOn(copyWebUi) }

dependencies {
    testImplementation("junit:junit:4.13.2")
    // core.aar is the Go engine (android/core), built by gomobile: see
    // android/build-core.sh.
    implementation(files("libs/core.aar"))
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("androidx.webkit:webkit:1.12.1")
}
