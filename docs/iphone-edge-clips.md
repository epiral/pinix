# iPhone Edge Clips -- Comprehensive Capability Report

> Date: 2026-03-23
> Context: Pinix V2 Architecture, iPhone as Edge Clip Provider
> Counterpart: macOS Edge Clips (25 clips)

## Architecture: clip-dock-ios

The iPhone connects to Hub as a single Provider (`clip-dock-ios`) that registers
multiple Edge Clips. It is a native SwiftUI app using the already-generated
Swift protobuf + Connect-RPC code at `gen/swift/pinix/v2/`.

```
clip-dock-ios (SwiftUI app)
  │
  ├── ProviderStream → Hub
  │     provider_name: "iphone-<device-id>"
  │     accepts_manage: false
  │
  └── Registers clips:
        iphone-camera
        iphone-health
        iphone-motion
        iphone-location
        ...
```

Key architectural facts:
- One Provider connection = one app process = N clips
- Heartbeat keeps all clips alive; app backgrounded = connection drops = clips offline
- Hub sees `iphone-camera`, `iphone-health`, etc. -- no special treatment
- Upper abstraction Clips (e.g. `camera`, `health`) unify iPhone + Mac capabilities

---

## Complete iPhone Edge Clip List (28 clips)

### 1. iphone-camera

**Framework:** AVFoundation, AVCaptureDevice, PhotoKit
**Domain:** Camera & Photography

| Command | Description |
|---------|------------|
| `take-photo` | Capture photo (front/back, ProRAW, ProRes, HDR) |
| `record-video` | Start/stop video recording (4K, Cinematic, SlowMo, TimeLapse) |
| `stream` | Live camera preview stream (for real-time analysis) |
| `switch-lens` | Switch between Wide/Ultra-Wide/Telephoto/Macro |
| `get-capabilities` | Report available lenses, formats, resolutions |
| `scan-document` | VisionKit document scanning |
| `scan-barcode` | QR/barcode detection via Vision framework |

**iPhone-unique:** Ultra-Wide + Telephoto lenses, Cinematic Mode, ProRAW/ProRes,
LiDAR-assisted autofocus, Macro mode, Photographic Styles, Action Mode stabilization.
Mac only has a fixed webcam.

### 2. iphone-photos

**Framework:** PhotoKit (PHPhotoLibrary, PHAsset)
**Domain:** Photo & Video Library

| Command | Description |
|---------|------------|
| `list` | List photos/videos with filters (date, album, media type) |
| `get` | Get photo/video data by asset ID |
| `create-album` | Create new album |
| `add-to-album` | Add assets to album |
| `search` | Search by date, location, people, scene (ML-powered) |
| `get-metadata` | EXIF, location, depth data |
| `save` | Save image/video to library |

**Overlap with macOS:** Same PhotoKit framework. Same commands. Abstraction Clip `photos` can unify.

### 3. iphone-health

**Framework:** HealthKit (HKHealthStore, HKSampleQuery)
**Domain:** Health & Fitness

| Command | Description |
|---------|------------|
| `query` | Query health data by type, date range, source |
| `get-steps` | Step count (today, range) |
| `get-heart-rate` | Heart rate samples |
| `get-sleep` | Sleep analysis data |
| `get-workouts` | Workout sessions (type, duration, calories, route) |
| `get-body` | Body measurements (weight, height, BMI, body fat) |
| `get-nutrition` | Dietary data (calories, macros, water) |
| `get-vitals` | Blood pressure, blood oxygen, respiratory rate, temperature |
| `get-reproductive` | Cycle tracking, fertility data |
| `get-mindfulness` | Mindfulness minutes, state of mind |
| `write` | Write health data (with user permission) |
| `get-trends` | Health trends and highlights |
| `list-sources` | List data sources (Watch, apps, devices) |

**iPhone-unique:** Entirely. macOS has no HealthKit. This is one of the most
valuable iPhone-only capabilities -- health data from Apple Watch flows through
iPhone's HealthKit.

Data types include:
- Activity: steps, distance, flights climbed, active energy, exercise minutes, stand hours
- Heart: heart rate, HRV, resting HR, walking HR, cardio fitness (VO2max)
- Body: weight, height, BMI, body fat %, lean body mass, waist circumference
- Vitals: blood pressure, blood oxygen, respiratory rate, body temperature
- Sleep: sleep stages (awake, REM, core, deep), time in bed
- Nutrition: calories, protein, carbs, fat, water, caffeine, 70+ nutrients
- Reproductive: menstrual cycles, ovulation, sexual activity
- Mobility: walking speed, step length, double support time, stair speed
- Mental: mindfulness minutes, state of mind
- Clinical: clinical records (FHIR), lab results, immunizations

### 4. iphone-motion

**Framework:** CoreMotion (CMMotionManager, CMPedometer, CMMotionActivityManager)
**Domain:** Motion Sensors

| Command | Description |
|---------|------------|
| `get-accelerometer` | Real-time accelerometer data (x, y, z) |
| `get-gyroscope` | Real-time gyroscope data (rotation rates) |
| `get-magnetometer` | Magnetometer / compass heading |
| `get-barometer` | Barometric pressure + relative altitude |
| `get-pedometer` | Steps, distance, pace, floors, cadence |
| `get-activity` | Current activity (stationary, walking, running, cycling, driving) |
| `get-device-motion` | Fused sensor data (attitude, rotation, gravity, acceleration) |
| `start-updates` | Stream continuous sensor data |
| `stop-updates` | Stop sensor streaming |

**iPhone-unique:** Full sensor suite. Mac has accelerometer on some models but
no gyroscope, no barometer, no pedometer, no activity recognition.

### 5. iphone-location

**Framework:** CoreLocation (CLLocationManager)
**Domain:** Location Services

| Command | Description |
|---------|------------|
| `get-location` | Current location (lat, lon, altitude, accuracy, speed, course) |
| `get-heading` | Compass heading (magnetic + true north) |
| `start-monitoring` | Continuous location updates |
| `monitor-region` | Geofence monitoring (enter/exit) |
| `monitor-beacons` | iBeacon ranging |
| `get-visit` | Visit detection (arrived/departed at places) |
| `reverse-geocode` | Coordinates to address |
| `forward-geocode` | Address to coordinates |

**Overlap with macOS:** macOS has CoreLocation but relies on WiFi positioning.
iPhone adds GPS + GLONASS + Galileo + BeiDou + cellular triangulation = much
higher accuracy. iPhone also has background location modes.

### 6. iphone-cellular

**Framework:** CoreTelephony (CTTelephonyNetworkInfo), CallKit, MessageFilter
**Domain:** Cellular & Telephony

| Command | Description |
|---------|------------|
| `get-carrier` | Current carrier info (name, country, network type) |
| `get-signal` | Radio access technology (5G/LTE/3G) |
| `get-data-usage` | Cellular data usage stats |
| `get-call-state` | Current call state (idle, ringing, connected) |
| `identify-caller` | CallKit caller ID lookup |
| `filter-sms` | MessageFilter for spam detection |
| `get-esim-status` | eSIM / dual SIM status |

**iPhone-unique:** Entirely. macOS has no cellular radio. Note: iOS does NOT allow
programmatic dialing or SMS sending without user confirmation (security restriction).

### 7. iphone-nfc

**Framework:** CoreNFC (NFCNDEFReaderSession, NFCTagReaderSession)
**Domain:** NFC

| Command | Description |
|---------|------------|
| `read-ndef` | Read NDEF tags (URL, text, custom payloads) |
| `write-ndef` | Write NDEF data to tags |
| `read-iso7816` | Read ISO 7816 smart cards |
| `read-iso15693` | Read ISO 15693 (NFC-V) tags |
| `read-felica` | Read FeliCa tags (Japan transit cards) |
| `read-mifare` | Read MIFARE tags |
| `detect-tags` | Background tag detection (NDEF only) |

**iPhone-unique:** Entirely. macOS has no NFC reader. Background tag reading
works even when app is not running (launches app on tag detection).

### 8. iphone-biometric

**Framework:** LocalAuthentication (LAContext)
**Domain:** Biometric Authentication

| Command | Description |
|---------|------------|
| `authenticate` | Trigger Face ID / Touch ID authentication |
| `get-type` | Query available biometric type (faceID, touchID, none) |
| `check-policy` | Check if biometric authentication is available |

**Overlap with macOS:** macOS has Touch ID on laptops. Same LAContext framework.
But iPhone has Face ID (TrueDepth camera system) which is unique.

### 9. iphone-haptic

**Framework:** CoreHaptics (CHHapticEngine), UIFeedbackGenerator
**Domain:** Haptic Feedback

| Command | Description |
|---------|------------|
| `impact` | Play impact haptic (light, medium, heavy, rigid, soft) |
| `notification` | Play notification haptic (success, warning, error) |
| `selection` | Play selection changed haptic |
| `custom` | Play custom haptic pattern (AHAP file or parameters) |
| `get-capabilities` | Check haptic engine capabilities |

**iPhone-unique:** Entirely. macOS has no Taptic Engine (except trackpad haptics
which are not programmable via CoreHaptics).

### 10. iphone-ar

**Framework:** ARKit, RealityKit, SceneKit
**Domain:** Augmented Reality

| Command | Description |
|---------|------------|
| `start-session` | Start AR session (world/face/body/image/object tracking) |
| `detect-planes` | Detect horizontal/vertical surfaces |
| `track-face` | Face tracking with 52 blendshapes |
| `track-body` | Full body pose estimation (3D skeleton) |
| `track-hand` | Hand tracking and gesture recognition |
| `scan-room` | RoomPlan room scanning (walls, doors, furniture, dimensions) |
| `capture-object` | Object Capture photogrammetry (3D model from photos) |
| `get-depth` | LiDAR depth map / scene reconstruction |
| `place-anchor` | Place virtual anchor at real-world position |
| `raycast` | Ray casting against detected surfaces |

**iPhone-unique:** Entirely. macOS has no ARKit, no TrueDepth, no LiDAR.
ARKit 6+ features: Location Anchors, 4K video capture, HDR, motion capture.

### 11. iphone-home

**Framework:** HomeKit (HMHomeManager)
**Domain:** Smart Home

| Command | Description |
|---------|------------|
| `list-homes` | List configured homes |
| `list-rooms` | List rooms in a home |
| `list-accessories` | List all accessories (lights, locks, thermostats, etc.) |
| `get-accessory` | Get accessory state (on/off, brightness, temperature, etc.) |
| `set-accessory` | Control accessory (turn on/off, set brightness, etc.) |
| `list-scenes` | List scenes (e.g., "Good Morning", "Movie Time") |
| `trigger-scene` | Trigger a scene |
| `list-automations` | List automations |
| `create-automation` | Create time/location/sensor-triggered automation |
| `get-cameras` | Get HomeKit Secure Video camera feeds |

**Overlap with macOS:** macOS also has HomeKit (via Home app). Same framework.
But iPhone is always with you, making location-based automations more practical.

### 12. iphone-contacts

**Framework:** Contacts (CNContactStore)
**Domain:** Contacts & Address Book

| Command | Description |
|---------|------------|
| `list` | List contacts with filters |
| `get` | Get contact details by ID |
| `search` | Search contacts by name, email, phone |
| `create` | Create new contact |
| `update` | Update contact fields |
| `delete` | Delete contact |
| `list-groups` | List contact groups |

**Overlap with macOS:** Same Contacts framework. Same iCloud sync. Abstraction
Clip `contacts` can unify.

### 13. iphone-calendar

**Framework:** EventKit (EKEventStore)
**Domain:** Calendar & Reminders

| Command | Description |
|---------|------------|
| `list-events` | List calendar events (date range, calendar filter) |
| `get-event` | Get event details |
| `create-event` | Create calendar event |
| `update-event` | Update event |
| `delete-event` | Delete event |
| `list-reminders` | List reminders (incomplete, completed, due) |
| `create-reminder` | Create reminder |
| `complete-reminder` | Mark reminder complete |
| `list-calendars` | List available calendars |

**Overlap with macOS:** Same EventKit framework. Same iCloud sync. Abstraction
Clip `calendar` can unify.

### 14. iphone-clipboard

**Framework:** UIPasteboard
**Domain:** Clipboard

| Command | Description |
|---------|------------|
| `get` | Get clipboard content (text, image, URL) |
| `set` | Set clipboard content |
| `get-types` | Get available pasteboard types |
| `clear` | Clear clipboard |

**Overlap with macOS:** Universal Clipboard syncs via Handoff. Same conceptual
API. Abstraction Clip `clipboard` can unify. Note: iOS 16+ shows clipboard
access notification to user.

### 15. iphone-notifications

**Framework:** UserNotifications (UNUserNotificationCenter), ActivityKit
**Domain:** Notifications & Live Activities

| Command | Description |
|---------|------------|
| `send-local` | Send local notification (title, body, badge, sound, actions) |
| `schedule` | Schedule notification (time, calendar, location trigger) |
| `cancel` | Cancel pending notification |
| `list-pending` | List pending notifications |
| `get-settings` | Get notification permission status |
| `start-live-activity` | Start Live Activity (Lock Screen + Dynamic Island) |
| `update-live-activity` | Update Live Activity content |
| `end-live-activity` | End Live Activity |

**iPhone-unique:** Live Activities + Dynamic Island are iPhone-only. Local
notifications exist on Mac but with different UX. Push notifications require
APNs infrastructure.

### 16. iphone-shortcuts

**Framework:** App Intents, IntentsExtension, Shortcuts
**Domain:** Automation & Shortcuts

| Command | Description |
|---------|------------|
| `list` | List user's shortcuts |
| `run` | Run a shortcut by name (with input parameters) |
| `list-intents` | List available App Intents from installed apps |
| `donate-intent` | Donate intent for Siri suggestions |

**Overlap with macOS:** Shortcuts exists on macOS too. Same App Intents framework.
But iPhone shortcuts have more triggers (NFC tag, arrive/leave location,
connect to CarPlay, open app, etc.).

### 17. iphone-wallet

**Framework:** PassKit (PKPaymentAuthorizationController, PKPassLibrary)
**Domain:** Wallet & Payments

| Command | Description |
|---------|------------|
| `check-apple-pay` | Check if Apple Pay is available |
| `list-passes` | List passes in Wallet (boarding passes, tickets, loyalty cards) |
| `add-pass` | Add pass to Wallet |
| `get-payment-methods` | List available payment cards |
| `request-payment` | Request Apple Pay payment (in-app) |

**iPhone-unique:** While Mac can do Apple Pay in Safari, iPhone has the full
Wallet with NFC payments, passes, car keys, ID cards, transit cards.

### 18. iphone-bluetooth

**Framework:** CoreBluetooth (CBCentralManager, CBPeripheralManager)
**Domain:** Bluetooth Low Energy

| Command | Description |
|---------|------------|
| `scan` | Scan for BLE peripherals |
| `connect` | Connect to a peripheral by UUID |
| `disconnect` | Disconnect from peripheral |
| `discover-services` | Discover GATT services |
| `read-characteristic` | Read a characteristic value |
| `write-characteristic` | Write to a characteristic |
| `subscribe` | Subscribe to characteristic notifications |
| `advertise` | Advertise as BLE peripheral |

**Overlap with macOS:** Same CoreBluetooth framework. Mac also has Bluetooth.
But iPhone is always-on and always carried, making it more practical for BLE
interactions (beacons, wearables, IoT devices).

### 19. iphone-uwb

**Framework:** NearbyInteraction (NISession, NINearbyObject)
**Domain:** Ultra-Wideband Spatial Awareness

| Command | Description |
|---------|------------|
| `start-session` | Start UWB session with nearby device |
| `get-nearby` | Get nearby UWB objects (distance + direction) |
| `precision-find` | Precision Finding for nearby devices |
| `stop-session` | Stop UWB session |

**iPhone-unique:** U1/U2 chip. macOS does not have UWB. Used for AirTag
precision finding, car key, HomePod handoff, device-to-device spatial awareness.

### 20. iphone-display

**Framework:** UIKit, UIScreen, ActivityKit
**Domain:** Display & UI

| Command | Description |
|---------|------------|
| `get-screen-info` | Screen size, scale, brightness, HDR capability |
| `set-brightness` | Set screen brightness |
| `get-dynamic-island` | Dynamic Island state and dimensions |
| `get-orientation` | Current device orientation |
| `get-appearance` | Dark/light mode, accessibility settings |
| `take-screenshot` | Capture screenshot |

**Overlap with macOS:** Both have screen info and screenshots. Dynamic Island
and Always-On Display are iPhone-only.

### 21. iphone-network

**Framework:** Network (NWPathMonitor, NWConnection), SystemConfiguration
**Domain:** Network Status

| Command | Description |
|---------|------------|
| `get-status` | Network connectivity status (WiFi, cellular, none) |
| `get-wifi-info` | Connected WiFi SSID and BSSID (with location permission) |
| `get-interfaces` | List network interfaces |
| `get-cellular-info` | Cellular network type (5G SA/NSA, LTE, etc.) |
| `monitor` | Stream network status changes |

**Overlap with macOS:** Both have Network framework. iPhone adds cellular
network info which Mac doesn't have.

### 22. iphone-audio

**Framework:** AVAudioEngine, AVAudioSession, Speech, SoundAnalysis
**Domain:** Audio & Speech

| Command | Description |
|---------|------------|
| `record` | Record audio from microphone |
| `play` | Play audio file or buffer |
| `transcribe` | On-device speech-to-text (Speech framework) |
| `classify-sound` | Sound classification (SoundAnalysis: dog bark, siren, etc.) |
| `get-audio-route` | Current audio route (speaker, headphones, Bluetooth) |
| `set-audio-session` | Configure audio session category |
| `text-to-speech` | Text-to-speech via AVSpeechSynthesizer |

**Overlap with macOS:** Both have AVAudioEngine, Speech framework. Same
capabilities mostly. Abstraction Clip `audio` can unify.

### 23. iphone-ml

**Framework:** CoreML, Vision, NaturalLanguage, Foundation Models, CreateML
**Domain:** On-Device Machine Learning

| Command | Description |
|---------|------------|
| `classify-image` | Image classification (Vision) |
| `detect-objects` | Object detection in image |
| `detect-text` | OCR / text recognition (VNRecognizeTextRequest) |
| `detect-faces` | Face detection and landmarks |
| `detect-body-pose` | Human body pose estimation |
| `detect-hand-pose` | Hand pose detection |
| `analyze-text` | NLP: sentiment, language, entities, tokenization |
| `translate` | On-device translation (Translation framework) |
| `generate-text` | On-device LLM via Foundation Models framework |
| `summarize` | Text summarization via Apple Intelligence |
| `run-model` | Run custom CoreML model |

**Overlap with macOS:** Very similar. macOS also has CoreML, Vision, Foundation
Models. Abstraction Clip `ml` can unify. iPhone adds Neural Engine optimization
for mobile inference.

### 24. iphone-system

**Framework:** UIDevice, ProcessInfo, UIApplication
**Domain:** Device System Info

| Command | Description |
|---------|------------|
| `get-info` | Device model, name, iOS version, processor |
| `get-battery` | Battery level, charging state |
| `get-storage` | Available/total storage |
| `get-thermal` | Thermal state (nominal, fair, serious, critical) |
| `get-memory` | Memory usage |
| `get-uptime` | System uptime |
| `is-low-power` | Low Power Mode status |
| `open-settings` | Open iOS Settings app (specific pane) |
| `open-url` | Open URL in default handler |

**Overlap with macOS:** Both have system info queries. iPhone adds battery,
thermal state, Low Power Mode.

### 25. iphone-accessibility

**Framework:** UIAccessibility, AVSpeechSynthesizer
**Domain:** Accessibility

| Command | Description |
|---------|------------|
| `get-settings` | VoiceOver, Dynamic Type, Reduce Motion, etc. |
| `announce` | Post accessibility announcement |
| `speak` | Speak text via VoiceOver or AVSpeechSynthesizer |
| `get-preferred-content-size` | Dynamic Type category |

**Limitation vs macOS:** On macOS, Accessibility API (AXUIElement) can inspect
and control ANY app's UI. On iOS, accessibility APIs are limited to your own
app. Cannot read or control other apps.

### 26. iphone-carplay

**Framework:** CarPlay (CPTemplateApplicationSceneDelegate)
**Domain:** CarPlay Integration

| Command | Description |
|---------|------------|
| `get-status` | CarPlay connection status |
| `get-session` | Current CarPlay session info |
| `send-alert` | Display alert on CarPlay screen |

**iPhone-unique:** Entirely. CarPlay requires iPhone tethered to car.

### 27. iphone-find-my

**Framework:** CoreLocation (background), FindMy network (limited API)
**Domain:** Find My Network

| Command | Description |
|---------|------------|
| `get-devices` | List devices in Find My (limited API availability) |
| `play-sound` | Play sound on device |

**Limitation:** Apple heavily restricts Find My API access. Third-party apps
cannot directly query Find My data. This clip would be limited in scope.

### 28. iphone-action-button

**Framework:** UIKit (Action Button events on iPhone 15 Pro+)
**Domain:** Hardware Button

| Command | Description |
|---------|------------|
| `get-config` | Current Action Button configuration |
| `on-press` | Register for Action Button press events |

**iPhone-unique:** Only on iPhone 15 Pro and later. Customizable hardware button.

---

## Comparison: macOS vs iPhone Edge Clips

### Clips with direct macOS equivalents (shared abstraction possible)

| iPhone Clip | macOS Clip | Abstraction Clip |
|------------|-----------|-----------------|
| iphone-camera | macos-camera | `camera` |
| iphone-photos | macos-photos | `photos` |
| iphone-location | macos-location | `location` |
| iphone-contacts | macos-pim | `contacts` |
| iphone-calendar | macos-pim | `calendar` |
| iphone-clipboard | macos-clipboard | `clipboard` |
| iphone-bluetooth | macos-network | `bluetooth` |
| iphone-audio | macos-mic + macos-audio + macos-sound | `audio` |
| iphone-ml | macos-ml + macos-llm + macos-translate | `ml` |
| iphone-system | macos-system | `system` |
| iphone-home | (macOS Home app) | `home` |
| iphone-shortcuts | macos-shortcuts | `shortcuts` |
| iphone-network | macos-network | `network` |
| iphone-display | macos-screen | `display` |
| iphone-notifications | macos-notifications | `notify` |
| iphone-biometric | macos-security | `auth` |
| iphone-accessibility | macos-accessibility | `accessibility` |

### iPhone-ONLY clips (no macOS equivalent)

| Clip | Why iPhone-only |
|------|----------------|
| **iphone-health** | HealthKit, Apple Watch data. No macOS equivalent. |
| **iphone-motion** | Accelerometer, gyroscope, barometer, pedometer. |
| **iphone-cellular** | 5G/LTE radio, carrier info, call state. |
| **iphone-nfc** | NFC tag reading/writing. |
| **iphone-haptic** | Taptic Engine haptic feedback. |
| **iphone-ar** | ARKit, LiDAR, TrueDepth camera. |
| **iphone-uwb** | Ultra-Wideband spatial awareness. |
| **iphone-wallet** | Wallet passes, NFC payments, car keys. |
| **iphone-carplay** | CarPlay vehicle integration. |
| **iphone-action-button** | Hardware Action Button (15 Pro+). |
| **iphone-find-my** | Find My network (limited). |

### macOS-ONLY clips (no iPhone equivalent)

| Clip | Why macOS-only |
|------|---------------|
| **macos-shell** | Terminal / shell execution. iOS is sandboxed. |
| **macos-filesystem** | Full filesystem access. iOS is sandboxed. |
| **macos-input** | Simulate keyboard/mouse input. iOS cannot. |
| **macos-accessibility** | AXUIElement to inspect/control ANY app. iOS cannot. |
| **macos-windows** | Window management (position, resize). iOS cannot. |
| **macos-apps** | Launch/quit/manage applications. iOS cannot. |
| **macos-browser** | Direct browser automation. iOS cannot. |
| **macos-vm** | Virtualization.framework. iOS cannot. |

---

## iOS-Specific Limitations & Workarounds

### 1. Background Execution

**Problem:** iOS aggressively suspends background apps. When clip-dock-ios goes
to background, the Hub connection drops and all clips go offline.

**Workarounds:**
- **Background Modes:** Register for audio, location, BLE, VoIP background modes
  to extend background execution time.
- **BGTaskScheduler:** Schedule background processing (max ~30s) and background
  app refresh (periodic, system-managed).
- **Push Notifications:** Use silent push (APNs) to wake the app briefly for
  data updates.
- **Background URL Session:** Network requests continue when app is suspended.
- **Persistent connection via VoIP push:** Can keep WebSocket alive (requires
  PushKit VoIP entitlement, Apple reviews this strictly).
- **Live Activity:** Keep app relevant via Lock Screen Live Activity while
  maintaining limited background execution.

**Recommended approach:** Accept that iPhone goes offline when backgrounded.
Design the system for graceful offline/online transitions. Use push notifications
to wake for critical operations. Consider background location mode if location
is the primary use case.

### 2. Sandboxed Filesystem

**Problem:** No access to other apps' files. No arbitrary filesystem browsing.

**Workaround:** Expose app sandbox, iCloud Drive, and Files app integration via
UIDocumentPickerViewController.

### 3. No Shell / Process Spawning

**Problem:** Cannot execute arbitrary commands or spawn processes.

**Impact:** No equivalent to `macos-shell`. All functionality must be built into
the app binary.

### 4. No Cross-App UI Automation

**Problem:** Cannot inspect or control other apps' UI elements. No Accessibility
API for foreign apps (unlike macOS AXUIElement).

**Impact:** No equivalent to `macos-accessibility`, `macos-input`, `macos-windows`,
`macos-apps`. The iPhone Edge Clip can only interact with iOS through official
APIs and its own app context.

### 5. User Permission Requirements

Many capabilities require explicit user permission:
- Camera, Microphone, Photos, Contacts, Calendar, Reminders, Health, Location,
  Bluetooth, NFC, Motion, HomeKit, Notifications, Face ID
- Permissions are requested at runtime via system dialogs
- User can revoke in Settings at any time
- clip-dock-ios must handle permission denied gracefully

### 6. App Store Review

**Problem:** clip-dock-ios must pass App Store Review. Apple restricts:
- Apps that are "shells" for web content
- Apps that download executable code
- Apps that access private APIs

**Workaround:** clip-dock-ios is a native SwiftUI app with clear user-facing
functionality. Edge Clip commands are handled by native Swift code, not
downloaded scripts.

### 7. Clipboard Access Notification

**Problem:** iOS 16+ shows a banner when an app reads the clipboard.

**Impact:** `iphone-clipboard get` will trigger a visible system notification.
Users may find this alarming. Consider clipboard access only on explicit user
action.

### 8. Rate Limits & Restrictions

- **HealthKit:** No background access (except Apple Watch complications).
  Queries require app to be in foreground or have active background mode.
- **Location:** Background location shows blue indicator. Frequent GPS polling
  drains battery heavily.
- **NFC:** Requires user-initiated scan (foreground only). Cannot scan silently.
- **Camera:** Foreground only. Cannot capture in background.
- **Shortcuts:** `run` command may require user confirmation for some actions.

---

## Required iOS Frameworks by Clip

| Clip | Frameworks | Entitlements / Capabilities |
|------|-----------|---------------------------|
| iphone-camera | AVFoundation, VisionKit | Camera Usage |
| iphone-photos | PhotoKit | Photo Library Usage |
| iphone-health | HealthKit | HealthKit Capability |
| iphone-motion | CoreMotion | Motion Usage |
| iphone-location | CoreLocation, MapKit | Location Usage (Always/When In Use) |
| iphone-cellular | CoreTelephony, CallKit | -- |
| iphone-nfc | CoreNFC | NFC Tag Reading, Near Field Communication |
| iphone-biometric | LocalAuthentication | Face ID Usage |
| iphone-haptic | CoreHaptics, UIKit | -- |
| iphone-ar | ARKit, RealityKit, SceneKit | Camera Usage |
| iphone-home | HomeKit | HomeKit Capability |
| iphone-contacts | Contacts, ContactsUI | Contacts Usage |
| iphone-calendar | EventKit | Calendar/Reminders Usage |
| iphone-clipboard | UIKit (UIPasteboard) | -- |
| iphone-notifications | UserNotifications, ActivityKit | Push Notifications, Live Activities |
| iphone-shortcuts | AppIntents, Intents | Siri |
| iphone-wallet | PassKit | Apple Pay, Wallet |
| iphone-bluetooth | CoreBluetooth | Bluetooth Usage |
| iphone-uwb | NearbyInteraction | Nearby Interaction |
| iphone-display | UIKit | -- |
| iphone-network | Network, SystemConfiguration | -- |
| iphone-audio | AVFoundation, Speech, SoundAnalysis | Microphone Usage, Speech Recognition |
| iphone-ml | CoreML, Vision, NaturalLanguage, Translation | -- |
| iphone-system | UIKit | -- |
| iphone-accessibility | UIKit (Accessibility) | -- |
| iphone-carplay | CarPlay | CarPlay Entitlement |
| iphone-find-my | CoreLocation | -- |
| iphone-action-button | UIKit | -- |

---

## Implementation Phasing

### Phase 1: MVP (Core value, low complexity)

**Priority clips to ship first (8 clips):**

1. **iphone-system** -- easiest, no permissions needed for basic info
2. **iphone-location** -- high value, straightforward CoreLocation
3. **iphone-camera** -- high value, AVFoundation
4. **iphone-photos** -- high value, PhotoKit
5. **iphone-contacts** -- iCloud synced, useful for AI agents
6. **iphone-calendar** -- iCloud synced, useful for scheduling
7. **iphone-clipboard** -- simple, enables cross-device paste
8. **iphone-notifications** -- enables Hub to push info to iPhone

**Why these first:** They demonstrate the core value proposition (phone as
remote sensor/actuator for AI agents), have well-documented APIs, and provide
immediate abstraction layer opportunities with macOS clips.

### Phase 2: Health & Sensors (Unique value)

9. **iphone-health** -- massive unique value, no Mac equivalent
10. **iphone-motion** -- unique sensor data
11. **iphone-network** -- connectivity awareness
12. **iphone-biometric** -- secure authentication flow

**Why Phase 2:** These are iPhone's strongest differentiators but require more
complex permission flows and data handling.

### Phase 3: Advanced Capabilities

13. **iphone-bluetooth** -- IoT integration
14. **iphone-nfc** -- physical world interaction
15. **iphone-haptic** -- feedback channel
16. **iphone-audio** -- voice input/output
17. **iphone-ml** -- on-device inference
18. **iphone-shortcuts** -- automation bridge
19. **iphone-home** -- smart home control

### Phase 4: Specialized

20. **iphone-ar** -- augmented reality (complex, niche)
21. **iphone-uwb** -- spatial awareness (requires UWB devices)
22. **iphone-wallet** -- payments (requires merchant setup)
23. **iphone-display** -- screen control
24. **iphone-carplay** -- vehicle (requires CarPlay entitlement)
25. **iphone-accessibility** -- limited on iOS
26. **iphone-action-button** -- hardware-specific
27. **iphone-find-my** -- heavily restricted API
28. **iphone-cellular** -- mostly read-only info

---

## clip-dock-ios App Architecture

```
clip-dock-ios/
  Sources/
    App/
      ClipDockApp.swift          -- SwiftUI app entry point
      ContentView.swift          -- Main UI (connection status, clip list)
      SettingsView.swift         -- Hub URL, device name, permissions
    Provider/
      ProviderConnection.swift   -- Connect-RPC ProviderStream management
      Heartbeat.swift            -- Heartbeat timer
      ClipRegistry.swift         -- Manages registered clips
      InvokeRouter.swift         -- Routes InvokeCommand to clip handlers
    Clips/
      CameraClip.swift
      HealthClip.swift
      MotionClip.swift
      LocationClip.swift
      ContactsClip.swift
      CalendarClip.swift
      ...
    Generated/
      pinix/v2/hub.pb.swift      -- (from gen/swift/pinix/v2/)
      pinix/v2/hub.connect.swift
  Package.swift                  -- SPM dependencies
```

**Key dependencies:**
- `apple/swift-protobuf` -- Protobuf runtime
- `connectrpc/connect-swift` -- Connect-RPC client
- `apple/swift-nio` -- Async networking (for gRPC transport)

**Registration example:**

```swift
let clips: [ClipRegistration] = [
    .init(name: "iphone-camera",    domain: "Camera",     commands: [...]),
    .init(name: "iphone-health",    domain: "Health",     commands: [...]),
    .init(name: "iphone-location",  domain: "Location",   commands: [...]),
    // ...
]

let register = RegisterRequest.with {
    $0.providerName = "clip-dock-ios-\(deviceID)"
    $0.acceptsManage = false
    $0.clips = clips
}
```

---

## Summary

| Metric | iPhone | macOS |
|--------|--------|-------|
| Total Edge Clips | 28 | 25 |
| iPhone-only | 11 | -- |
| macOS-only | -- | 8 |
| Shared (need abstraction) | 17 | 17 |
| Requires user permission | 19 | ~10 |
| Background-capable | Limited | Full |
| App Store required | Yes | No |

**iPhone's unique strengths:** Health data, motion sensors, cellular, NFC,
haptics, AR/LiDAR, UWB, wallet/payments, CarPlay, always-with-you location.

**iPhone's key limitations:** No shell, no cross-app automation, no filesystem,
background execution restrictions, App Store review, permission-heavy.

**Architectural insight:** iPhone and macOS are complementary Edge Clips.
Together they cover the full spectrum of a user's digital+physical life.
The abstraction Clip pattern (e.g., `camera` Clip depending on both
`iphone-camera` and `macos-camera`) is essential for upper layers to work
seamlessly regardless of which device is currently online.
