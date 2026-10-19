package io.github.yourfriendaaron.meatmode

import android.app.Application

/** Sets up what every process of the app needs before any screen, service or receiver runs. */
class MeatApp : Application() {
    override fun onCreate() {
        super.onCreate()
        CrashLog.install(this)
    }
}
