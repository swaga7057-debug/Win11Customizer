# Безопасные улучшения Win11 Customizer

Дата проверки источников: 2026-11-19. Область исследования: Windows 11, настольное Go-приложение, персонализация, обои, Проводник и панель задач.

## Короткий вывод

Для новых функций лучше предпочесть документированные Win32/COM API и Policy CSP, а не прямую запись во внутренние ключи `Explorer\Advanced`. Наиболее полезные ближайшие улучшения: транзакционный откат, обои для отдельных мониторов и слайд-шоу через `IDesktopWallpaper`, автоскрытие панели задач через `SHAppBarMessage`, доступные системные анимации через `SystemParametersInfo`, а также отдельный «управляемый режим» для поддерживаемых device policies.

Ниже «подтверждено» означает, что интерфейс, параметр и семантика опубликованы Microsoft Learn/Support. Это не означает, что любая запись в упомянутую ветку реестра автоматически является поддерживаемым пользовательским API.

## Рекомендуемые функции

### 1. Транзакционный снимок и точный Undo для каждого изменения

**Статус:** подтверждённые Win32 Registry API; это наиболее важное улучшение безопасности.

Перед изменением значения читать его через `RegGetValueW` (тип, размер и исходные байты), а затем писать через `RegSetKeyValueW`/`RegSetValueExW`. Журнал должен различать два исходных состояния: «значение существовало» и «значения не было». В первом случае откат восстанавливает исходные тип и данные, во втором — удаляет только созданное приложением значение через `RegDeleteValueW`. Нельзя считать фиксированное значение `Off` универсальным откатом: пользовательская исходная настройка могла отличаться.

- Права: для `HKCU` обычно достаточно `KEY_QUERY_VALUE | KEY_SET_VALUE` текущего пользователя; для защищённых ключей `HKLM` обычно потребуется elevation. Запрашивать только минимальные права, а не `KEY_ALL_ACCESS`.
- Откат: воспроизвести снимок в обратном порядке; для составного твика откатывать уже применённые операции при первой ошибке.
- Практическая защита: записывать журнал до мутации, хранить тип (`REG_DWORD`, `REG_SZ` и т. п.) и сырые данные, после записи перечитывать значение.

Источники Microsoft: [`RegGetValueW`](https://learn.microsoft.com/en-us/windows/win32/api/winreg/nf-winreg-reggetvaluew), [`RegSetKeyValueW`](https://learn.microsoft.com/en-us/windows/win32/api/winreg/nf-winreg-regsetkeyvaluew), [`RegDeleteValueW`](https://learn.microsoft.com/en-us/windows/win32/api/winreg/nf-winreg-regdeletevaluew), [Registry Key Security and Access Rights](https://learn.microsoft.com/en-us/windows/win32/sysinfo/registry-key-security-and-access-rights).

### 2. Разные обои для разных мониторов

**Статус:** подтверждённый COM API, Windows 8+; подходит для Windows 11.

Создать `IDesktopWallpaper` через `CoCreateInstance(CLSID_DesktopWallpaper, ..., CLSCTX_ALL, IID_IDesktopWallpaper, ...)`. Получить число мониторов через `GetMonitorDevicePathCount`, идентификаторы — `GetMonitorDevicePathAt`, затем вызвать:

```text
IDesktopWallpaper::SetWallpaper(monitorID, absoluteImagePath)
```

`monitorID == NULL` применяет изображение ко всем мониторам. Путь должен быть абсолютным. В Go COM-вызовы следует выполнять на OS thread с корректными `CoInitializeEx`/`CoUninitialize`; возвращаемые `HRESULT` нельзя игнорировать.

- Права: обычный интерактивный пользователь; elevation не нужен.
- Откат: до изменения сохранить `GetWallpaper(monitorID)` для каждого ID и вернуть сохранённые пути. Учитывать подключение/отключение мониторов и не привязывать профиль только к порядковому номеру.

Источники Microsoft: [`IDesktopWallpaper`](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nn-shobjidl_core-idesktopwallpaper), [`SetWallpaper`](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-idesktopwallpaper-setwallpaper), [`GetMonitorDevicePathAt`](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-idesktopwallpaper-getmonitordevicepathat), [COM initialization](https://learn.microsoft.com/en-us/windows/win32/learnwin32/initializing-the-com-library).

### 3. Позиция, цвет фона и слайд-шоу обоев без ручного редактирования реестра

**Статус:** подтверждённый `IDesktopWallpaper` API.

Добавить режимы `DWPOS_CENTER`, `DWPOS_TILE`, `DWPOS_STRETCH`, `DWPOS_FIT`, `DWPOS_FILL`, `DWPOS_SPAN` через `SetPosition`. Цвет свободной области задаётся `SetBackgroundColor(COLORREF)`. Слайд-шоу задаётся `SetSlideshow(IShellItemArray)` и `SetSlideshowOptions(DESKTOP_SLIDESHOW_OPTIONS, slideshowTick)`, где `slideshowTick` — интервал между переходами в миллисекундах.

- Права: обычный пользователь.
- Откат: сохранить `GetPosition`, `GetBackgroundColor`, `GetSlideshow` и `GetSlideshowOptions`, затем восстановить их теми же методами. Если снимок слайд-шоу не удалось получить, не применять составное изменение.
- Совместимость: использовать `SystemParametersInfoW(SPI_SETDESKWALLPAPER, ...)` только как простой fallback для одного общего изображения; для него `SPIF_UPDATEINIFILE` сохраняет настройку в профиле, `SPIF_SENDCHANGE` рассылает `WM_SETTINGCHANGE`.

Источники Microsoft: [`SetPosition`](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-idesktopwallpaper-setposition), [`DESKTOP_WALLPAPER_POSITION`](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/ne-shobjidl_core-desktop_wallpaper_position), [`SetBackgroundColor`](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-idesktopwallpaper-setbackgroundcolor), [`SetSlideshow`](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-idesktopwallpaper-setslideshow), [`SetSlideshowOptions`](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-idesktopwallpaper-setslideshowoptions), [`SystemParametersInfoW`](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-systemparametersinfow).

### 4. Автоматически скрывать панель задач

**Статус:** подтверждённые Shell-сообщения, но реализацию нужно тестировать на нескольких мониторах и актуальных сборках Windows 11.

Прочитать текущее состояние:

```text
SHAppBarMessage(ABM_GETSTATE, &APPBARDATA)
```

Флаг `ABS_AUTOHIDE` означает включённое автоскрытие. Для изменения использовать `ABM_SETSTATE`; `APPBARDATA.cbSize` обязателен, а для `ABM_SETSTATE` документация также требует `hWnd`, желаемое состояние передаётся в `lParam`. Не перезапускать `explorer.exe`, если API вернул ожидаемый результат.

- Права: обычный пользователь.
- Откат: сохранить полный результат `ABM_GETSTATE`, а не только boolean, и вернуть исходные биты через `ABM_SETSTATE`.
- Ограничение: Microsoft документирует сообщения, но не публикует стабильный контракт для поиска taskbar HWND по имени класса `Shell_TrayWnd`; если такой поиск понадобится, изолировать его как совместимый fallback и не называть полностью поддерживаемым API.

Источники Microsoft: [`SHAppBarMessage`](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shappbarmessage), [`ABM_GETSTATE`](https://learn.microsoft.com/en-us/windows/win32/shell/abm-getstate), [`ABM_SETSTATE`](https://learn.microsoft.com/en-us/windows/win32/shell/abm-setstate), [`APPBARDATA`](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/ns-shellapi-appbardata).

### 5. «Уменьшить анимации» как функция доступности

**Статус:** подтверждённый Win32 API.

Использовать `SystemParametersInfoW` с `SPI_GETCLIENTAREAANIMATION` (`0x1042`) для чтения и `SPI_SETCLIENTAREAANIMATION` (`0x1043`) для записи `BOOL`. Microsoft прямо описывает этот параметр как управление анимациями и transient effects клиентской области и связывает его с доступностью для людей с фоточувствительной эпилепсией. При записи использовать `SPIF_UPDATEINIFILE | SPIF_SENDCHANGE`.

- Права: обычный пользователь.
- Откат: вернуть ранее прочитанный `BOOL` тем же `SPI_SETCLIENTAREAANIMATION`.
- UI: не обещать отключение абсолютно всех анимаций Windows; документированный охват — client-area animations/transient effects.

Источник Microsoft: [`SystemParametersInfoW`](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-systemparametersinfow) (см. `SPI_GETCLIENTAREAANIMATION`, `SPI_SETCLIENTAREAANIMATION`, `SPIF_*`).

### 6. Скрывать/показывать значки рабочего стола через Shell API

**Статус:** документирован, но Microsoft предупреждает, что `SHGetSetSettings` может измениться или исчезнуть в будущих версиях; поэтому функция должна быть помечена как version-sensitive.

Вызвать `SHGetSetSettings(&state, SSF_HIDEICONS, FALSE)` для чтения `SHELLSTATE.fHideIcons`, затем с нужным `fHideIcons` вызвать функцию с `bSet=TRUE`. `SSF_HIDEICONS == 0x00004000` документирован Microsoft.

- Права: обычный пользователь.
- Откат: вернуть исходный `fHideIcons`.
- Проверка: после записи повторно прочитать состояние; при несовпадении показать «не поддерживается этой сборкой», не пытаться исправлять внутренним ключом реестра.

Источники Microsoft: [`SHGetSetSettings`](https://learn.microsoft.com/en-us/windows/win32/api/shlobj_core/nf-shlobj_core-shgetsetsettings), [SSF constants](https://learn.microsoft.com/en-us/windows/win32/shell/ssf-constants), [`SHELLSTATE`](https://learn.microsoft.com/en-us/windows/win32/api/shlobj_core/ns-shlobj_core-shellstatea).

### 7. Управляемый переключатель Widgets для Pro/Enterprise

**Статус:** подтверждённая device policy, не обычная пользовательская настройка.

Policy CSP:

```text
OMA-URI: ./Device/Vendor/MSFT/Policy/Config/NewsAndInterests/AllowNewsAndInterests
Format: int
0 = widgets not allowed
1 = allowed (default)
Group Policy registry mapping:
HKLM\SOFTWARE\Policies\Microsoft\Dsh
  AllowNewsAndInterests (REG_DWORD)
```

Политика действует на весь Widgets experience, включая контент панели задач. Применима с Windows 11 21H2 к Pro, Enterprise, Education и IoT Enterprise. В интерфейсе её нужно отделить от текущего per-user «скрыть кнопку»: policy запрещает функцию на устройстве и сообщает Windows, что настройкой управляет организация.

- Права: администратор/MDM, потому что scope — Device и запись `HKLM`.
- Откат: удалить policy value (Not configured), а не принудительно писать `1`; затем перечитать эффективное состояние. Если устройство управляется доменом/MDM, не перетирать внешнюю политику.

Источник Microsoft: [Policy CSP — NewsAndInterests / AllowNewsAndInterests](https://learn.microsoft.com/en-us/windows/client-management/mdm/policy-csp-newsandinterests#allownewsandinterests).

### 8. Управляемый режим отображения Search на панели задач (Windows 11 24H2+)

**Статус:** подтверждённая device policy с узким диапазоном версий; не применять на старых сборках.

```text
OMA-URI: ./Device/Vendor/MSFT/Policy/Config/Search/ConfigureSearchOnTaskbarMode
Format: int
0 = Hide
1 = Search icon only
2 = Search icon and label
3 = Search box (default)
```

Поддержка: Windows 11 24H2 (`10.0.26100`) и новее, Pro/Enterprise/Education/IoT Enterprise, Device scope. Learn указывает Group Policy mapping к `Computer Configuration > Windows Components > Search`, файлу `Search.admx` и ключу `Software\Policies\Microsoft\Windows\Windows Search`, но на странице не публикует Registry Value Name. Поэтому приложение не должно угадывать имя значения: применять через Policy CSP/Group Policy или оставить функцию как переход в Settings.

- Права: администратор/MDM.
- Откат: удалить policy (Not configured), чтобы пользователь снова мог выбирать режим в Settings.

Источник Microsoft: [Policy CSP — Search / ConfigureSearchOnTaskbarMode](https://learn.microsoft.com/en-us/windows/client-management/mdm/policy-csp-search#configuresearchontaskbarmode).

### 9. «Открыть в Параметрах Windows» для настроек без поддерживаемого setter API

**Статус:** подтверждённый Microsoft URI scheme; наиболее безопасный путь для темы, цвета и части панели задач.

Запускать URI через `ShellExecuteW`/обычный protocol launch:

```text
ms-settings:personalization
ms-settings:personalization-background
ms-settings:personalization-colors
ms-settings:themes
ms-settings:personalization-start
ms-settings:taskbar
```

Это особенно полезно там, где приложение сейчас знает только внутренний ключ реестра, но Microsoft не документирует его как API. Кнопка может жить рядом с экспериментальным твиком и давать поддерживаемую альтернативу.

- Права: обычный пользователь.
- Откат: не требуется — переход сам ничего не меняет.
- Ошибки: проверять результат запуска; доступность отдельных страниц зависит от версии/редакции Windows.

Источники Microsoft: [Launch the Windows Settings app](https://learn.microsoft.com/en-us/windows/apps/develop/launch/launch-settings), [`ShellExecuteW`](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecutew).

## Что в текущем приложении нельзя считать подтверждённым контрактом

Следующие параметры популярны в сценариях настройки Windows, но в найденных первичных документах Microsoft Learn/Support нет нормативного контракта, гарантирующего их пользовательскую семантику на Windows 11. Их допустимо оставить только с меткой **«экспериментально / зависит от сборки»**, проверкой результата и точным снимком для Undo:

- `HKCU\Software\Classes\CLSID\{86ca1aa0-34aa-4e8b-a509-50c905bae2a2}\InprocServer32` для «классического» контекстного меню;
- `Explorer\Advanced\TaskbarSi`, `TaskbarAl`, `TaskbarGlomLevel`, `TaskbarDa`, `TaskbarMn`, `ShowCopilotButton`, `TaskbarBadges`, `MMTaskbarEnabled`;
- `Explorer\EnableAutoTray`, `Explorer\Advanced\ShowTaskViewButton`, `TaskbarDeveloperSettings\TaskbarEndTask`;
- `Themes\Personalize\AppsUseLightTheme`, `SystemUsesLightTheme`, `EnableTransparency`, `ColorPrevalence` как прямой setter-контракт;
- `ContentDeliveryManager\SubscribedContent-*` с числовыми именами;
- прямые пользовательские ключи `AdvertisingInfo\Enabled` и `Privacy\TailoredExperiencesWithDiagnosticDataEnabled`, если они выдаются за эквивалент документированной политики;
- `Control Panel\Desktop\WallpaperStyle`/`TileWallpaper` для новых функций — вместо них есть документированный `IDesktopWallpaper::SetPosition`.

Отсутствие публичной документации не доказывает, что ключ «не работает»; оно означает, что Microsoft не обещает его стабильность, допустимые значения, момент применения или поведение отката. Microsoft Q&A, ответы сообщества и сторонние «tweak lists» не следует считать первичным контрактом.

## Предлагаемый порядок реализации

1. Сначала общий снимок/Undo и проверка результата для всех операций.
2. Затем заменить текущую установку обоев на `IDesktopWallpaper` и добавить per-monitor/position/slideshow.
3. Добавить API-функции доступности и Shell (`SystemParametersInfo`, `SHGetSetSettings`, `SHAppBarMessage`) с runtime-проверкой.
4. Вынести device policies в отдельный раздел «Для администраторов», не смешивать их с персональными твиками.
5. Все неподтверждённые registry tweaks пометить как экспериментальные и рядом дать переход на соответствующую страницу `ms-settings:`.

