# Android

Android-клиент — это тот же клиент, что на Windows, собранный под телефон:

- **Интерфейс** — тот же Vue-интерфейс (`frontend/`) внутри WebView. Вместо
  Wails-привязок страница зовёт мост `JorbiqueAndroid` (`frontend/src/lib/android.js`
  и `android/app/.../Bridge.kt`) под теми же именами методов.
- **Логика** — Go-пакет `android/core`: загрузка профиля, проверка UUID, автовыбор
  (Naive первым, иначе самый быстрый, urltest с переключением между проверенными),
  сборка конфигурации и IP-карточка берутся из `internal/` десктопного клиента.
- **Движок** — sing-box-extended (тот же коммит, что на сервере) внутри приложения
  через libbox, а не отдельным процессом. Каждая проверка протокола поднимает
  движок с SOCKS-входом на loopback, туннель поднимается после выбора.
- **VPN** — `TunnelService` (VpnService). Он открывает интерфейс по запросу движка,
  выводит сокеты движка из туннеля (`protect`), сообщает движку текущую сеть и
  отвечает на DNS-запросы профиля к `local`-серверу через системный резолвер.

## Что отличается от Windows

| | Windows | Android |
|---|---|---|
| TUN | wintun, адрес 172.31.255.1/30 | VpnService, 172.19.0.1/30 и IPv6-адрес, MTU 9000 |
| Исключённые приложения | `process_name` / `process_path` | `exclude_package` (система не пускает их трафик в туннель) |
| Автозапуск | при входе в Windows | подключение после включения телефона |
| sing-box | скачивается при первом запуске | встроен в APK |

Сайты в обход VPN, автовыбор протокола, список протоколов и IP-карточка работают
так же, как на Windows.

## Сборка

Нужны Go (версия из `android/core/go.mod`), Java 17, Android SDK с NDK и
`SERVER_HOST` / `SERVER_CHANNEL` (как для остальных сборок).

```bash
(cd frontend && npm ci && npm run build)
ANDROID_NDK_HOME=/path/to/ndk bash android/build-core.sh   # android/app/libs/core.aar
(cd android && ./gradlew assembleRelease)
```

APK получается в `android/app/build/outputs/apk/release/`. Собирается только
arm64-v8a, минимальная версия Android 10.

Подпись берётся из `ANDROID_KEYSTORE_FILE`, `ANDROID_KEYSTORE_PASSWORD`,
`ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`. В CI это секреты
`ANDROID_KEYSTORE_BASE64` (keystore в base64) и три остальных. Без них APK
подписывается отладочным ключом, а релиз не публикуется: обновление поверх
APK с другой подписью не встанет.
