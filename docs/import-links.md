# Ссылки jorbique://

Кнопка «Открыть в Jorbique VPN» на странице подписки `/sub/<UUID>` открывает
установленный клиент ссылкой

```
jorbique://import?uuid=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

Клиент проверяет UUID на сервере, сохраняет его и готов к подключению. Из ссылки
читается только UUID правильного формата, всё остальное игнорируется: такую
ссылку может открыть любая страница.

| Платформа | Где регистрируется схема | Кто принимает ссылку |
|-----------|--------------------------|----------------------|
| Windows | `HKCU\Software\Classes\jorbique` при каждом запуске (`internal/deeplink/register_windows.go`) | второй, неповышенный экземпляр кладёт UUID в `%APPDATA%\JorbiqueVPN\pending_import_uuid` и выходит; запущенный клиент подхватывает файл (`watchImportLinks` в `cmd/jorbiquevpn/main.go`) |
| macOS | `CFBundleURLTypes` в `Info.plist` (`build-macos.sh`), `lsregister` в `postinstall` | `application:openURLs:` делегата приложения → `internal/macos/ui/importlink.go`, сохранение через демон (`set-singbox-config`) |
| Android | `intent-filter` с `scheme="jorbique" host="import"` у `MainActivity` | `MainActivity.importUUID`, страница забирает UUID через `TakePendingImportUUID` |

На Windows и Android UUID сохраняет общая страница (`AppShell.vue`): на экране
первого запуска поле заполняется и отправляется само, в работающем приложении UUID
заменяется так же, как в окне «UUID пользователя», и открытое подключение
переподключается.

Проверить вручную:

- Windows: `start jorbique://import?uuid=<UUID>`
- macOS: `open 'jorbique://import?uuid=<UUID>'`
- Android: `adb shell am start -a android.intent.action.VIEW -d 'jorbique://import?uuid=<UUID>'`
