// VideoDownloaderApp — нативная оболочка macOS для v-down (бриф 01, зона A).
//
// Окно NSWindow + WKWebView, сам Go-сервер живёт дочерним процессом: бинарник
// v-down лежит в Resources и запускается с «-addr 127.0.0.1:0», адрес читается
// из его вывода, готовность проверяется опросом /api/browsers.
//
// Три вещи, ради которых здесь не хватило Foundation.Process:
//   1) дочерний процесс поднимается через posix_spawn с POSIX_SPAWN_SETSID —
//      он становится лидером своей группы, и по Cmd+Q мы гасим сигналом всю
//      группу разом: ни yt-dlp, ни ffmpeg не остаются сиротами;
//   2) рабочая папка процесса задаётся явно (внутри .app писать некуда);
//   3) в PATH подставляется заглушка `open`, иначе сервер при старте открыл бы
//      ещё и вкладку браузера — см. комментарий у shimDir.
//
// Сборка: make app (swiftc, без Xcode-проекта).

import AppKit
import WebKit
import UniformTypeIdentifiers

// MARK: - Запуск дочернего процесса

/// Результат posix_spawn: pid дочернего процесса и конец пайпа, куда он пишет
/// stdout и stderr.
private struct SpawnedProcess {
    let pid: pid_t
    let outputFD: Int32
}

/// Запускает программу отдельной сессией (setsid), с заданными окружением и
/// рабочей папкой. stdout и stderr сведены в один пайп.
private func spawnDetached(path: String, args: [String],
                           env: [String: String], cwd: String) -> SpawnedProcess? {
    var fds: [Int32] = [0, 0]
    guard pipe(&fds) == 0 else { return nil }
    let readFD = fds[0], writeFD = fds[1]

    var actions: posix_spawn_file_actions_t?
    posix_spawn_file_actions_init(&actions)
    // Вариант без _np появился только в macOS 26, а бандл собирается под 13.0.
    posix_spawn_file_actions_addchdir_np(&actions, cwd)
    posix_spawn_file_actions_adddup2(&actions, writeFD, STDOUT_FILENO)
    posix_spawn_file_actions_adddup2(&actions, writeFD, STDERR_FILENO)
    posix_spawn_file_actions_addclose(&actions, readFD)
    posix_spawn_file_actions_addclose(&actions, writeFD)

    var attr: posix_spawnattr_t?
    posix_spawnattr_init(&attr)
    // Своя сессия: pgid дочернего процесса равен его pid, и kill(-pid) достаёт
    // всех его потомков, а не только его самого.
    posix_spawnattr_setflags(&attr, Int16(POSIX_SPAWN_SETSID))

    var argv: [UnsafeMutablePointer<CChar>?] = ([path] + args).map { strdup($0) }
    argv.append(nil)
    var envp: [UnsafeMutablePointer<CChar>?] = env.map { strdup("\($0.key)=\($0.value)") }
    envp.append(nil)
    defer {
        argv.forEach { free($0) }
        envp.forEach { free($0) }
        posix_spawn_file_actions_destroy(&actions)
        posix_spawnattr_destroy(&attr)
    }

    var pid: pid_t = 0
    let rc = posix_spawn(&pid, path, &actions, &attr, argv, envp)
    close(writeFD) // пишущий конец нужен только ребёнку
    guard rc == 0 else {
        close(readFD)
        return nil
    }
    return SpawnedProcess(pid: pid, outputFD: readFD)
}

// MARK: - ServerController

/// Управляет процессом v-down: запуск, чтение вывода в лог, ожидание
/// готовности, остановка вместе со всеми потомками.
final class ServerController {
    enum StartError: LocalizedError {
        case noBundle, spawnFailed, exitedEarly, timeout
        var errorDescription: String? {
            switch self {
            case .noBundle:
                return "не удалось подготовить движок v-down (Contents/Resources)"
            case .spawnFailed:
                return "не удалось создать процесс движка"
            case .exitedEarly:
                return "движок завершился при старте (подробности в \(ServerController.logPath))"
            case .timeout:
                return "движок не ответил вовремя (подробности в \(ServerController.logPath))"
            }
        }
    }

    static let logPath = NSHomeDirectory() + "/Library/Logs/video-downloader.log"

    private(set) var baseURL: URL?
    /// Папка, из которой запущен сервер: относительные пути от него (downloads/…)
    /// разворачиваются именно отсюда.
    private(set) var workDir: String = NSHomeDirectory()

    private var pid: pid_t = 0
    private var stopping = false
    private let exited = DispatchSemaphore(value: 0)
    private var reaped = false

    /// Зовётся на главной очереди, если сервер умер сам.
    var onUnexpectedExit: (() -> Void)?
    /// Текст для надписи «что происходит» на время старта.
    var onStatus: ((String) -> Void)?

    private var handle: FileHandle?
    private var logHandle: FileHandle?
    private let logQueue = DispatchQueue(label: "video-downloader.log")
    private var scanBuffer = Data()
    private var urlFound = false
    /// Момент последней активности процесса: пока он что-то печатает (а на
    /// первом запуске он качает ~80 МБ FFmpeg), таймаут не наступает.
    private var lastActivity = Date()

    func start(completion: @escaping (Result<URL, Error>) -> Void) {
        stopping = false
        reaped = false
        baseURL = nil
        urlFound = false
        scanBuffer.removeAll()
        lastActivity = Date()

        var finished = false
        let finish: (Result<URL, Error>) -> Void = { result in
            DispatchQueue.main.async {
                guard !finished else { return }
                finished = true
                completion(result)
            }
        }

        guard let resources = Bundle.main.resourceURL else {
            finish(.failure(StartError.noBundle))
            return
        }
        guard let exe = Self.prepareEngine(resources: resources) else {
            finish(.failure(StartError.noBundle))
            return
        }
        openLog()

        workDir = Self.prepareWorkDir()
        // -no-open: интерфейс уже показан в этом окне, вкладка браузера лишняя.
        // -addr с нулевым портом: систему просим выдать любой свободный.
        guard let child = spawnDetached(path: exe,
                                        args: ["-no-open", "-addr", "127.0.0.1:0"],
                                        env: ProcessInfo.processInfo.environment,
                                        cwd: workDir) else {
            finish(.failure(StartError.spawnFailed))
            return
        }
        pid = child.pid
        log("движок запущен, pid \(child.pid), папка \(workDir)")

        let handle = FileHandle(fileDescriptor: child.outputFD, closeOnDealloc: true)
        self.handle = handle
        handle.readabilityHandler = { [weak self] h in
            let data = h.availableData
            if data.isEmpty {
                h.readabilityHandler = nil
                return
            }
            self?.consume(data) { url in
                self?.waitUntilHealthy(url, deadline: Date().addingTimeInterval(30)) { ok in
                    finish(ok ? .success(url) : .failure(StartError.timeout))
                }
            }
        }

        // Отдельный поток ждёт смерти процесса: это и «упал на старте», и
        // «упал в работе», и подтверждение остановки при выходе.
        DispatchQueue.global().async { [weak self] in
            var status: Int32 = 0
            while waitpid(child.pid, &status, 0) < 0 && errno == EINTR { continue }
            guard let self else { return }
            self.reaped = true
            self.exited.signal()
            self.log("движок завершился, код \(status)")
            DispatchQueue.main.async {
                guard !self.stopping else { return }
                if finished {
                    self.onUnexpectedExit?()
                } else {
                    finish(.failure(StartError.exitedEarly))
                }
            }
        }

        watchStartup(finish: finish)
    }

    /// Сторож старта: ждём адрес, но отсчитываем таймаут не от запуска, а от
    /// последней строки вывода — первая установка зависимостей идёт минутами.
    private func watchStartup(finish: @escaping (Result<URL, Error>) -> Void) {
        DispatchQueue.main.asyncAfter(deadline: .now() + 5) { [weak self] in
            guard let self, !self.urlFound else { return }
            if Date().timeIntervalSince(self.lastActivity) > 45 {
                finish(.failure(StartError.timeout))
                return
            }
            self.watchStartup(finish: finish)
        }
    }

    /// SIGTERM всей группе процессов, через 3 секунды — SIGKILL. Звать с
    /// главного потока при выходе из программы.
    func stop() {
        stopping = true
        guard pid > 0, !reaped else { return }
        kill(-pid, SIGTERM)
        if exited.wait(timeout: .now() + 3) == .timedOut {
            log("движок не завершился за 3 с — SIGKILL группе")
            kill(-pid, SIGKILL)
            _ = exited.wait(timeout: .now() + 2)
        }
        pid = 0
    }

    // MARK: окружение дочернего процесса

    /// Движок копируется из бандла в ~/Library/Application Support и запускается
    /// оттуда. Причина: v-down кладёт скачанные yt-dlp и FFmpeg в папку tools/
    /// рядом с собой, а запись внутрь .app ломает подпись бандла и теряется при
    /// каждом обновлении программы.
    ///
    /// Копия считается свежей, только если совпали и размер, и дата: размера
    /// одного мало — правка вёрстки легко даёт бинарник тех же байт, и
    /// обновление молча не доехало бы до пользователя.
    private static func prepareEngine(resources: URL) -> String? {
        let fm = FileManager.default
        let source = resources.appendingPathComponent("v-down")
        guard fm.fileExists(atPath: source.path) else { return nil }

        let dir = NSHomeDirectory() + "/Library/Application Support/Video Downloader"
        let target = URL(fileURLWithPath: dir).appendingPathComponent("v-down")

        func stamp(_ path: String) -> String? {
            guard let a = try? fm.attributesOfItem(atPath: path),
                  let size = a[.size] as? UInt64,
                  let date = a[.modificationDate] as? Date else { return nil }
            return "\(size)-\(date.timeIntervalSince1970)"
        }

        do {
            try fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
            if stamp(source.path) != stamp(target.path) {
                if fm.fileExists(atPath: target.path) { try fm.removeItem(at: target) }
                try fm.copyItem(at: source, to: target)
                try fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: target.path)
            }
            return target.path
        } catch {
            // Не смогли скопировать — работаем прямо из бандла, это лучше, чем
            // не запуститься вовсе.
            return source.path
        }
    }

    /// Папка загрузок: писать внутрь .app нельзя, поэтому сервер работает из
    /// ~/Downloads/Video Downloader, а файлы кладёт в downloads/ внутри неё.
    private static func prepareWorkDir() -> String {
        let dir = NSHomeDirectory() + "/Downloads/Video Downloader"
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        var isDir: ObjCBool = false
        if FileManager.default.fileExists(atPath: dir, isDirectory: &isDir), isDir.boolValue {
            return dir
        }
        return NSHomeDirectory()
    }

    // MARK: вывод процесса

    private func consume(_ data: Data, onURL: @escaping (URL) -> Void) {
        logQueue.sync { logHandle?.write(data) }
        lastActivity = Date()

        let chunk = String(decoding: data, as: UTF8.self)
        if chunk.contains("скачиваю") {
            DispatchQueue.main.async {
                self.onStatus?("Первый запуск: докачиваю yt-dlp и FFmpeg…")
            }
        }

        guard !urlFound else { return }
        scanBuffer.append(data)
        if scanBuffer.count > 64 * 1024 {
            scanBuffer.removeFirst(scanBuffer.count - 64 * 1024)
        }
        let text = String(decoding: scanBuffer, as: UTF8.self)
        // Адрес печатается в цветной строке «Откройте в браузере: …», поэтому
        // ищем сам URL, а не подпись вокруг него.
        guard let range = text.range(of: #"http://[0-9A-Za-z\.\:\[\]]+"#, options: .regularExpression),
              let url = URL(string: String(text[range])) else { return }
        urlFound = true
        DispatchQueue.main.async {
            self.baseURL = url
            self.onStatus?("Почти готово…")
            onURL(url)
        }
    }

    /// Опрос /api/browsers: адрес печатается раньше, чем сервер начинает отвечать.
    private func waitUntilHealthy(_ base: URL, deadline: Date, done: @escaping (Bool) -> Void) {
        var req = URLRequest(url: base.appendingPathComponent("api/browsers"))
        req.timeoutInterval = 2
        URLSession.shared.dataTask(with: req) { _, response, _ in
            if let http = response as? HTTPURLResponse, http.statusCode == 200 {
                done(true)
                return
            }
            if Date() >= deadline {
                done(false)
                return
            }
            DispatchQueue.global().asyncAfter(deadline: .now() + 0.2) {
                self.waitUntilHealthy(base, deadline: deadline, done: done)
            }
        }.resume()
    }

    // MARK: лог

    private func openLog() {
        guard logHandle == nil else { return }
        let dir = NSHomeDirectory() + "/Library/Logs"
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        if !FileManager.default.fileExists(atPath: Self.logPath) {
            FileManager.default.createFile(atPath: Self.logPath, contents: nil)
        }
        // 4 МБ — дальше начинаем файл заново, чтобы лог не рос бесконечно.
        if let attrs = try? FileManager.default.attributesOfItem(atPath: Self.logPath),
           let size = attrs[.size] as? UInt64, size > 4 << 20 {
            try? FileManager.default.removeItem(atPath: Self.logPath)
            FileManager.default.createFile(atPath: Self.logPath, contents: nil)
        }
        logHandle = FileHandle(forWritingAtPath: Self.logPath)
        logHandle?.seekToEndOfFile()
        log("--- Video Downloader.app запуск \(ISO8601DateFormatter().string(from: Date())) ---")
    }

    private func log(_ text: String) {
        logQueue.sync { logHandle?.write(Data((text + "\n").utf8)) }
    }

    /// Пометка в общем логе от оболочки (не от сервера).
    func note(_ text: String) { log("[оболочка] " + text) }
}

// MARK: - WebView с настоящим drag-n-drop

/// WKWebView сам обрабатывает перетаскивание файлов, но веб-странице отдаёт
/// только имя — системный путь браузерам недоступен. Нам путь нужен (его надо
/// отдать серверу на транскрибацию), поэтому перехватываем drop до WebKit и
/// передаём путь в страницу сами.
final class DropWebView: WKWebView {
    var onDropFiles: (([String]) -> Void)?
    var onDragActive: ((Bool) -> Void)?

    private func filePaths(_ info: NSDraggingInfo) -> [String] {
        let opts: [NSPasteboard.ReadingOptionKey: Any] = [.urlReadingFileURLsOnly: true]
        guard let urls = info.draggingPasteboard.readObjects(forClasses: [NSURL.self],
                                                            options: opts) as? [URL] else { return [] }
        return urls.map { $0.path }
    }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        if filePaths(sender).isEmpty { return super.draggingEntered(sender) }
        onDragActive?(true)
        return .copy
    }

    override func draggingUpdated(_ sender: NSDraggingInfo) -> NSDragOperation {
        if filePaths(sender).isEmpty { return super.draggingUpdated(sender) }
        return .copy
    }

    override func draggingExited(_ sender: NSDraggingInfo?) {
        onDragActive?(false)
        super.draggingExited(sender)
    }

    override func prepareForDragOperation(_ sender: NSDraggingInfo) -> Bool {
        filePaths(sender).isEmpty ? super.prepareForDragOperation(sender) : true
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        let paths = filePaths(sender)
        onDragActive?(false)
        if paths.isEmpty { return super.performDragOperation(sender) }
        onDropFiles?(paths)
        return true
    }
}

// MARK: - AppDelegate

final class AppDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKScriptMessageHandler {
    private let server = ServerController()
    private var window: NSWindow?
    private var webView: DropWebView?
    private var statusLabel: NSTextField?
    private var navigationFailed = false
    private var reloadAttempts = 0
    private var signalSources: [DispatchSourceSignal] = []

    func applicationDidFinishLaunching(_ notification: Notification) {
        buildMenu()
        makeWindow()
        server.onUnexpectedExit = { [weak self] in self?.serverDied() }
        server.onStatus = { [weak self] text in self?.setStatus(text) }
        startServer()

        NSWorkspace.shared.notificationCenter.addObserver(
            self, selector: #selector(didWake),
            name: NSWorkspace.didWakeNotification, object: nil)

        // kill/выход из системы тоже должны гасить движок, а не оставлять сироту.
        for sig in [SIGTERM, SIGINT] {
            signal(sig, SIG_IGN)
            let src = DispatchSource.makeSignalSource(signal: sig, queue: .main)
            src.setEventHandler { NSApp.terminate(nil) }
            src.resume()
            signalSources.append(src)
        }
        NSApp.activate(ignoringOtherApps: true)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
    func applicationWillTerminate(_ notification: Notification) { server.stop() }
    func applicationSupportsSecureRestorableState(_ app: NSApplication) -> Bool { true }

    // MARK: окно

    private func makeWindow() {
        let win = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1120, height: 760),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false)
        win.title = "Video Downloader"
        win.contentMinSize = NSSize(width: 900, height: 620)
        win.center()
        win.setFrameAutosaveName("VideoDownloaderMainWindow")
        win.isReleasedWhenClosed = false

        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "app")
        let wv = DropWebView(frame: .zero, configuration: config)
        wv.navigationDelegate = self
        // Пинч-зум выключен: интерфейс не карта, а масштаб есть в меню «Вид».
        wv.allowsMagnification = false
        wv.isHidden = true
        wv.onDropFiles = { [weak self] paths in self?.deliverDrop(paths) }
        wv.onDragActive = { [weak self] on in
            self?.callJS("window.__nativeDragActive(\(on ? "true" : "false"))")
        }

        let label = NSTextField(labelWithString: "Запуск движка…")
        label.font = NSFont.systemFont(ofSize: 15)
        label.textColor = .secondaryLabelColor
        label.translatesAutoresizingMaskIntoConstraints = false

        if let content = win.contentView {
            wv.frame = content.bounds
            wv.autoresizingMask = [.width, .height]
            content.addSubview(wv)
            content.addSubview(label)
            NSLayoutConstraint.activate([
                label.centerXAnchor.constraint(equalTo: content.centerXAnchor),
                label.centerYAnchor.constraint(equalTo: content.centerYAnchor),
            ])
        }
        win.makeKeyAndOrderFront(nil)

        window = win
        webView = wv
        statusLabel = label
    }

    private func setStatus(_ text: String?) {
        if let text {
            statusLabel?.stringValue = text
            statusLabel?.isHidden = false
        } else {
            statusLabel?.isHidden = true
            webView?.isHidden = false
        }
    }

    // MARK: сервер

    private func startServer() {
        setStatus("Запуск движка…")
        server.start { [weak self] result in
            guard let self else { return }
            switch result {
            case .success(let url): self.webView?.load(URLRequest(url: url))
            case .failure(let err): self.startFailed(err)
            }
        }
    }

    private func startFailed(_ error: Error) {
        setStatus("Движок не запустился")
        let alert = NSAlert()
        alert.messageText = "Не удалось запустить движок"
        alert.informativeText = error.localizedDescription
        alert.addButton(withTitle: "Повторить")
        alert.addButton(withTitle: "Выйти")
        NSApp.activate(ignoringOtherApps: true)
        if alert.runModal() == .alertFirstButtonReturn { startServer() } else { NSApp.terminate(nil) }
    }

    private func serverDied() {
        let alert = NSAlert()
        alert.messageText = "Движок Video Downloader остановился"
        alert.informativeText = "Фоновый процесс неожиданно завершился. Подробности — в \(ServerController.logPath)."
        alert.addButton(withTitle: "Перезапустить")
        alert.addButton(withTitle: "Выйти")
        NSApp.activate(ignoringOtherApps: true)
        if alert.runModal() == .alertFirstButtonReturn { startServer() } else { NSApp.terminate(nil) }
    }

    // MARK: мост со страницей

    func userContentController(_ controller: WKUserContentController,
                               didReceive message: WKScriptMessage) {
        guard let body = message.body as? [String: Any],
              let action = body["action"] as? String else { return }
        switch action {
        case "reveal":
            reveal(path: body["path"] as? String ?? "")
        case "revealDownloads":
            let dir = URL(fileURLWithPath: server.workDir).appendingPathComponent("downloads")
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            NSWorkspace.shared.open(dir)
        case "pickFile":
            pickFile()
        case "theme":
            applyAppearance(body["value"] as? String ?? "auto")
        default:
            break
        }
    }

    /// Рамка окна рисуется системой, а тему выбирают в интерфейсе — держим их
    /// в согласии, иначе светлая страница живёт в тёмном заголовке.
    private func applyAppearance(_ theme: String) {
        switch theme {
        case "light": NSApp.appearance = NSAppearance(named: .aqua)
        case "dark":  NSApp.appearance = NSAppearance(named: .darkAqua)
        default:      NSApp.appearance = nil // как в системе
        }
    }

    /// Относительные пути (downloads/видео.mp4) сервер отдаёт как есть —
    /// разворачиваем их от его рабочей папки.
    private func absolute(_ path: String) -> URL {
        if path.hasPrefix("/") { return URL(fileURLWithPath: path) }
        return URL(fileURLWithPath: server.workDir).appendingPathComponent(path)
    }

    private func reveal(path: String) {
        guard !path.isEmpty else { return }
        let url = absolute(path)
        if FileManager.default.fileExists(atPath: url.path) {
            NSWorkspace.shared.activateFileViewerSelecting([url])
        } else {
            // Файл могли переместить или удалить — открываем хотя бы папку.
            NSWorkspace.shared.open(url.deletingLastPathComponent())
        }
    }

    private func pickFile() {
        let panel = NSOpenPanel()
        panel.title = "Выберите видео или аудио"
        panel.allowsMultipleSelection = false
        panel.canChooseDirectories = false
        panel.allowedContentTypes = [.movie, .video, .audio, .mpeg4Movie, .mp3, .quickTimeMovie]
        panel.directoryURL = URL(fileURLWithPath: server.workDir).appendingPathComponent("downloads")
        NSApp.activate(ignoringOtherApps: true)
        guard panel.runModal() == .OK, let url = panel.url else { return }
        deliverDrop([url.path])
    }

    private func deliverDrop(_ paths: [String]) {
        guard let first = paths.first else { return }
        callJS("window.__nativePicked(\(jsString(first)))")
    }

    /// Строка для вставки в JS: JSON-кодирование само экранирует кавычки,
    /// слеши и юникод — путь может быть каким угодно.
    private func jsString(_ value: String) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: [value], options: []),
              let text = String(data: data, encoding: .utf8) else { return "\"\"" }
        return String(text.dropFirst().dropLast()) // убираем скобки массива
    }

    private func callJS(_ source: String) {
        webView?.evaluateJavaScript(source, completionHandler: nil)
    }

    // MARK: навигация

    func webView(_ webView: WKWebView,
                 decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = navigationAction.request.url else {
            decisionHandler(.cancel)
            return
        }
        if url.scheme == "about" || isOurs(url) {
            decisionHandler(.allow)
        } else {
            decisionHandler(.cancel)
        }
    }

    private func isOurs(_ url: URL) -> Bool {
        guard let base = server.baseURL else { return false }
        let host = url.host ?? ""
        return url.scheme == "http" && (host == "127.0.0.1" || host == "localhost") && url.port == base.port
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        navigationFailed = false
        reloadAttempts = 0
        setStatus(nil)
        snapshotIfRequested()
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        server.note("навигация не удалась: \(error.localizedDescription)")
        scheduleReload()
    }

    func webView(_ webView: WKWebView,
                 didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        server.note("навигация не началась: \(error.localizedDescription)")
        scheduleReload()
    }

    /// Процесс отрисовки страницы упал (обычно нехватка памяти). WKWebView в
    /// этот момент показывает пустоту и сам не восстанавливается — грузим заново.
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        server.note("процесс страницы завершился — перезагружаем интерфейс")
        reloadAttempts = 0
        navigationFailed = true
        reloadIfNeeded()
    }

    private func scheduleReload() {
        navigationFailed = true
        guard reloadAttempts < 30 else { return }
        reloadAttempts += 1
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) { [weak self] in self?.reloadIfNeeded() }
    }

    private func reloadIfNeeded() {
        guard navigationFailed, let base = server.baseURL else { return }
        webView?.load(URLRequest(url: base))
    }

    @objc private func didWake() {
        if navigationFailed {
            reloadAttempts = 0
            reloadIfNeeded()
        }
    }

    @objc private func reloadPage() {
        guard let base = server.baseURL else { return }
        webView?.load(URLRequest(url: base))
    }

    @objc private func openDownloads() {
        let dir = URL(fileURLWithPath: server.workDir).appendingPathComponent("downloads")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        NSWorkspace.shared.open(dir)
    }

    /// Масштаб интерфейса: tag = шаг (+1 крупнее, −1 мельче, 0 — сброс).
    @objc private func zoomPage(_ sender: NSMenuItem) {
        guard let wv = webView else { return }
        switch sender.tag {
        case 0: wv.pageZoom = 1
        default: wv.pageZoom = min(3, max(0.5, wv.pageZoom + Double(sender.tag) * 0.1))
        }
    }

    // MARK: снимок окна для документации

    /// Отладочный хук: VDOWN_SNAPSHOT=/путь.png — снять своё окно после
    /// загрузки. screencapture требует разрешение «Запись экрана», а своё окно
    /// приложение отрисует само: рамка через cacheDisplay, контент через
    /// takeSnapshot, композиция в PNG. VDOWN_SNAPSHOT_JS подготавливает
    /// состояние интерфейса, VDOWN_SNAPSHOT_DELAY — пауза в секундах.
    private func snapshotIfRequested() {
        let env = ProcessInfo.processInfo.environment
        guard let path = env["VDOWN_SNAPSHOT"], !path.isEmpty else { return }
        let delay = Double(env["VDOWN_SNAPSHOT_DELAY"] ?? "") ?? 2.5
        if let js = env["VDOWN_SNAPSHOT_JS"],
           let source = try? String(contentsOfFile: js, encoding: .utf8) {
            webView?.evaluateJavaScript(source, completionHandler: nil)
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            // Окно за чужими окнами macOS не перерисовывает, и в снимок попадает
            // пустота: выводим его вперёд и даём кадр на отрисовку.
            NSApp.activate(ignoringOtherApps: true)
            self?.window?.orderFrontRegardless()
            // Первый takeSnapshot часто отдаёт кадр без слоёв, которые WebKit
            // держит отдельно (анимации, только что показанные блоки). Греем
            // холостым вызовом и снимаем со второго раза.
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.8) {
                let warmup = WKSnapshotConfiguration()
                warmup.afterScreenUpdates = true
                self?.webView?.takeSnapshot(with: warmup) { _, _ in
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) {
                        self?.snapshotWindow(to: path)
                    }
                }
            }
        }
    }

    private func snapshotWindow(to path: String) {
        guard let win = window, let wv = webView,
              let frameView = win.contentView?.superview,
              let frameRep = frameView.bitmapImageRepForCachingDisplay(in: frameView.bounds)
        else { return }
        frameView.cacheDisplay(in: frameView.bounds, to: frameRep)
        // afterScreenUpdates обязателен: без него takeSnapshot отдаёт уже
        // скомпонованный кадр, в котором нет слоёв с активными анимациями —
        // страница на снимке получается наполовину пустой.
        let config = WKSnapshotConfiguration()
        config.afterScreenUpdates = true
        wv.takeSnapshot(with: config) { webImage, _ in
            let size = win.frame.size
            let composed = NSImage(size: size)
            composed.lockFocus()
            frameRep.draw(in: NSRect(origin: .zero, size: size))
            if let webImage {
                webImage.draw(in: wv.convert(wv.bounds, to: nil))
            }
            composed.unlockFocus()
            guard let tiff = composed.tiffRepresentation,
                  let rep = NSBitmapImageRep(data: tiff),
                  let png = rep.representation(using: .png, properties: [:])
            else { return }
            try? png.write(to: URL(fileURLWithPath: path))
            if ProcessInfo.processInfo.environment["VDOWN_SNAPSHOT_QUIT"] == "1" {
                DispatchQueue.main.async { NSApp.terminate(nil) }
            }
        }
    }

    // MARK: меню

    /// Меню собирается кодом. Меню «Правка» обязательно: без него в WKWebView
    /// не работают Cmd+C/V/X/A.
    private func buildMenu() {
        let main = NSMenu()

        let appItem = NSMenuItem()
        main.addItem(appItem)
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "О программе Video Downloader",
                        action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Скрыть Video Downloader",
                        action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        let hideOthers = NSMenuItem(title: "Скрыть остальные",
                                    action: #selector(NSApplication.hideOtherApplications(_:)),
                                    keyEquivalent: "h")
        hideOthers.keyEquivalentModifierMask = [.command, .option]
        appMenu.addItem(hideOthers)
        appMenu.addItem(.separator())
        appMenu.addItem(withTitle: "Завершить Video Downloader",
                        action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu

        let fileItem = NSMenuItem(title: "Файл", action: nil, keyEquivalent: "")
        main.addItem(fileItem)
        let fileMenu = NSMenu(title: "Файл")
        let openItem = NSMenuItem(title: "Открыть папку загрузок",
                                  action: #selector(openDownloads), keyEquivalent: "o")
        fileMenu.addItem(openItem)
        fileItem.submenu = fileMenu

        let editItem = NSMenuItem(title: "Правка", action: nil, keyEquivalent: "")
        main.addItem(editItem)
        let editMenu = NSMenu(title: "Правка")
        editMenu.addItem(withTitle: "Отменить", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "Повторить", action: Selector(("redo:")), keyEquivalent: "Z")
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: "Вырезать", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Скопировать", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Вставить", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Выделить всё",
                         action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu

        let viewItem = NSMenuItem(title: "Вид", action: nil, keyEquivalent: "")
        main.addItem(viewItem)
        let viewMenu = NSMenu(title: "Вид")
        viewMenu.addItem(withTitle: "Обновить", action: #selector(reloadPage), keyEquivalent: "r")
        viewMenu.addItem(.separator())
        for (title, key, tag) in [("Крупнее", "+", 1), ("Мельче", "-", -1),
                                  ("Фактический размер", "0", 0)] {
            let item = NSMenuItem(title: title, action: #selector(zoomPage(_:)), keyEquivalent: key)
            item.tag = tag
            viewMenu.addItem(item)
        }
        viewItem.submenu = viewMenu

        let windowItem = NSMenuItem(title: "Окно", action: nil, keyEquivalent: "")
        main.addItem(windowItem)
        let windowMenu = NSMenu(title: "Окно")
        windowMenu.addItem(withTitle: "Свернуть",
                           action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Масштабировать",
                           action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        windowItem.submenu = windowMenu
        NSApp.windowsMenu = windowMenu

        NSApp.mainMenu = main
    }
}

// MARK: - точка входа

// Второй запуск при живом экземпляре: поднимаем уже открытое окно и выходим,
// второй сервер не нужен.
let currentPID = NSRunningApplication.current.processIdentifier
if let other = NSRunningApplication.runningApplications(withBundleIdentifier: "dev.rocket.go-yt-dwn")
    .first(where: { $0.processIdentifier != currentPID }) {
    if #available(macOS 14.0, *) {
        other.activate()
    } else {
        other.activate(options: [.activateIgnoringOtherApps])
    }
    exit(0)
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()
