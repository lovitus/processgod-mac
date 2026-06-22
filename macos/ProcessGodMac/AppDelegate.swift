import AppKit
import SwiftUI

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private let model = AppModel()
    private var statusItem: NSStatusItem!
    private let popover = NSPopover()
    private var windows: [NSWindow] = []
    private var statusTimer: Timer?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: "bolt.horizontal.circle.fill", accessibilityDescription: "ProcessGod")
            button.target = self; button.action = #selector(togglePopover)
        }
        popover.behavior = .transient
        popover.contentSize = NSSize(width: 420, height: 560)
        popover.contentViewController = NSHostingController(rootView: PopoverView(model: model, openManager: { [weak self] id in self?.openManager(id) }, openLogs: { [weak self] id in self?.openLogs(id) }, openSettings: { [weak self] in self?.openSettings() }))
        statusTimer = .scheduledTimer(withTimeInterval: 2, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.updateStatusIcon() }
        }
        let isTesting = ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil || ProcessInfo.processInfo.arguments.contains("--uitesting")
        if !isTesting { Task { await model.start(); updateStatusIcon() } }
        if ProcessInfo.processInfo.arguments.contains("--uitesting") { openManager(nil) }
    }

    func applicationWillTerminate(_ notification: Notification) {
        statusTimer?.invalidate()
    }

    @objc private func togglePopover() {
        guard let button = statusItem.button else { return }
        if popover.isShown { popover.performClose(nil) }
        else { popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY); popover.contentViewController?.view.window?.makeKey() }
    }

    private func openManager(_ id: String?) {
        popover.performClose(nil)
        let view = ManagerView(model: model, initialSelection: id, openLogs: { [weak self] processID in self?.openLogs(processID) })
        showWindow(title: model.localized("manager.title"), size: NSSize(width: 1080, height: 700), root: AnyView(view))
    }

    private func openLogs(_ id: String) {
        popover.performClose(nil)
        showWindow(title: model.localized("logs.title") + " - " + id, size: NSSize(width: 800, height: 560), root: AnyView(LogsView(model: model, processID: id)))
    }

    func openSettings() {
        popover.performClose(nil)
        showWindow(title: model.localized("settings.title"), size: NSSize(width: 560, height: 460), root: AnyView(SettingsView(model: model)))
    }

    private func showWindow(title: String, size: NSSize, root: AnyView) {
        NSApp.setActivationPolicy(.regular)
        let controller = NSHostingController(rootView: root)
        let window = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        window.title = title; window.contentViewController = controller; window.delegate = self
        window.center(); windows.append(window); window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func windowWillClose(_ notification: Notification) {
        guard let closing = notification.object as? NSWindow else { return }
        windows.removeAll { $0 === closing }
        if windows.isEmpty { NSApp.setActivationPolicy(.accessory) }
    }

    private func updateStatusIcon() {
        let running = model.runtime.processes.filter { $0.state == .running }.count
        let enabled = model.config.processes.filter(\.enabled).count
        let level = model.localized(model.serviceSelection == .system ? "service.system.short" : "service.user.short")
        let symbol: String
        let color: NSColor?
        if model.connectionError != nil {
            symbol = "exclamationmark.triangle.fill"
            color = .systemRed
        } else if model.config.guardianPaused {
            symbol = "pause.circle.fill"
            color = .systemOrange
        } else if running > 0 {
            symbol = "bolt.horizontal.circle.fill"
            color = nil
        } else {
            symbol = "bolt.horizontal.circle"
            color = .secondaryLabelColor
        }
        statusItem.button?.image = NSImage(systemSymbolName: symbol, accessibilityDescription: "ProcessGod")
        statusItem.button?.contentTintColor = color
        statusItem.button?.toolTip = model.connectionError ?? "ProcessGod | \(level) | \(running)/\(enabled)"
        statusItem.button?.appearsDisabled = false
    }
}
