# Sign the Android app

The release workflow builds `meat-mode-android-vX.Y.Z.apk` when you push a tag `vX.Y.Z`. It signs the APK with a key only you hold. This page is the one-time setup. Until all four secrets exist, a tag build fails on purpose, so no unsigned or debug-signed release is ever published.

You need `keytool` (it ships with any JDK), `base64`, the [GitHub CLI](https://cli.github.com) signed in with rights to the repository, and an Android SDK for `apksigner` (or any APK built with the key; see step 5).

## 1. Make the key

Pick a directory outside the repository, so the key can never be committed. Choose strong passwords and keep them in your password manager.

```sh
mkdir -p ~/meat-mode-signing && cd ~/meat-mode-signing
keytool -genkeypair -v \
  -keystore meat-mode-release.keystore \
  -alias meatmode \
  -keyalg RSA -keysize 4096 \
  -validity 36500 \
  -dname "CN=Meat Mode"
```

`keytool` asks for the keystore password and the key password (press Enter at the second prompt to reuse the first). You should see `[Storing meat-mode-release.keystore]`. A validity of 36500 days is about 100 years; Android needs the certificate to last past 2033.

## 2. Print the certificate fingerprint

```sh
keytool -list -v -keystore meat-mode-release.keystore -alias meatmode | grep 'SHA256:'
```

You should see one line, `SHA256: AB:CD:...`, 32 pairs of hex digits. This is the value to publish in the README next to the package ID `io.github.yourfriendaaron.meatmode`, for people who check the app with AppVerifier.

## 3. Store the key as repository secrets

Run these in the repository (add `--repo <owner>/<repo>` elsewhere). Each prompts for its value or reads a pipe, so nothing lands in your shell history.

```sh
base64 -w0 meat-mode-release.keystore | gh secret set MEAT_MODE_ANDROID_KEYSTORE_BASE64
gh secret set MEAT_MODE_ANDROID_KEYSTORE_PASSWORD
gh secret set MEAT_MODE_ANDROID_KEY_ALIAS --body meatmode
gh secret set MEAT_MODE_ANDROID_KEY_PASSWORD
```

On macOS use `base64 -i meat-mode-release.keystore | tr -d '\n'` for the first command's input. Check with `gh secret list`: all four names appear.

## 4. Back up the key outside CI

Copy `meat-mode-release.keystore` and both passwords to two places that are not GitHub: for example an encrypted password-manager attachment and an encrypted offline drive.

GitHub secrets cannot be read back. If you lose the keystore or its passwords, you can never sign an update that Android accepts as the same app. Every user must uninstall the app, lose its local settings, and install a new one signed with a new key, and the published certificate fingerprint changes. Keep the backup for as long as the app has users.

## 5. Check the first release

After you push the first tag, download the APK from the release and run:

```sh
apksigner verify --print-certs meat-mode-android-vX.Y.Z.apk
```

(`apksigner` is in the Android SDK's `build-tools/<version>/` directory.) The `Signer #1 certificate SHA-256 digest` must equal the fingerprint from step 2. The workflow runs the same command and prints it in the job log. Then replace the placeholder fingerprint in the README.

## 6. Clean up

```sh
shred -u meat-mode-release.keystore   # only after the backups in step 4 are checked
```

The workflow decodes the keystore into a temporary directory on the runner and deletes it at the end of the job, also when the build fails.
