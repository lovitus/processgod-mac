import SwiftUI

struct PopoverView: View {
    @Bindable var model: AppModel
    let openManager: (String?) -> Void
    let openLogs: (String) -> Void
    let openSettings: () -> Void
    @State private var previewProcessID: String?
    @State private var preview: LogSnapshot?
    @State private var previewError: String?

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            if let previewProcessID { logPreview(processID: previewProcessID) }
            else if model.config.processes.isEmpty { emptyState }
            else { processList }
            Divider()
            footer
        }
        .frame(width: 420, height: 560)
        .environment(\.locale, model.locale)
        .task(id: model.logEventGeneration) {
            guard let previewProcessID else { return }
            try? await Task.sleep(for: .milliseconds(preview == nil ? 0 : 150))
            guard !Task.isCancelled else { return }
            await loadPreview(previewProcessID)
        }
    }

    private var header: some View {
        VStack(spacing: 10) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text("app.title").font(.headline)
                    Text(model.localized(model.runtime.healthy ? "guardian.connected" : "guardian.degraded"))
                        .font(.caption).foregroundStyle(model.runtime.healthy ? Color.secondary : Color.red)
                }
                Spacer()
                Text(model.localized(model.serviceSelection == .system ? "service.system.short" : "service.user.short"))
                    .font(.caption.weight(.semibold)).padding(.horizontal, 8).padding(.vertical, 4)
                    .background(.quaternary, in: Capsule())
            }
            HStack {
                Label("\(summary.running)/\(summary.enabled)", systemImage: "waveform.path.ecg")
                    .font(.subheadline.monospacedDigit())
                Spacer()
                Button(model.localized(model.config.guardianPaused ? "guardian.resume" : "guardian.pause")) {
                    Task { await model.setPaused(!model.config.guardianPaused) }
                }
                .buttonStyle(.borderedProminent)
            }
            if let error = model.connectionError {
                Text(error).font(.caption).foregroundStyle(.red).lineLimit(2).frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(16)
    }

    private var processList: some View {
        ScrollView {
            LazyVStack(spacing: 7) {
                ForEach(model.config.processes) { process in
                    ProcessPopoverRow(
                        process: process,
                        runtime: model.runtime.processes.first { $0.id == process.id },
                        toggle: { Task { await model.setEnabled(process.id, enabled: !process.enabled) } },
                        restart: { Task { await model.restart(process.id) } },
                        logs: {
                            previewProcessID = process.id
                            preview = nil
                            previewError = nil
                            Task { await loadPreview(process.id) }
                        },
                        edit: { openManager(process.id) }
                    )
                }
            }
            .padding(10)
        }
    }

    private var emptyState: some View {
        ContentUnavailableView("process.empty", systemImage: "gearshape.2", description: Text("process.empty.description"))
            .frame(maxHeight: .infinity)
    }

    private func logPreview(processID: String) -> some View {
        VStack(spacing: 0) {
            HStack {
                Button { previewProcessID = nil; preview = nil } label: { Label("common.back", systemImage: "chevron.left") }
                Spacer()
                Text(model.config.processes.first(where: { $0.id == processID })?.name ?? processID).font(.headline)
                Spacer()
                Button("logs.openFull") { openLogs(processID) }
            }
            .padding(12)
            Divider()
            if let preview {
                HStack {
                    Label("\(preview.errorWarning.kept)/\(preview.errorWarning.capacity)", systemImage: "exclamationmark.triangle")
                    Spacer()
                    Label("\(preview.standardOther.kept)/\(preview.standardOther.capacity)", systemImage: "text.alignleft")
                    Spacer()
                    Text("<=\(preview.lineMaxBytes) B")
                }
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
                .padding(10)
                let entries = (preview.errorWarning.entries + preview.standardOther.entries).sorted { $0.sequence < $1.sequence }.suffix(14)
                List(Array(entries)) { entry in
                    VStack(alignment: .leading, spacing: 2) {
                        HStack {
                            Text("#\(entry.sequence)").monospacedDigit()
                            Text(entry.source)
                            Spacer()
                            Text(entry.timestamp, style: .time)
                        }
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        Text(entry.text).font(.system(.caption, design: .monospaced)).lineLimit(3).textSelection(.enabled)
                    }
                }
            } else if let previewError {
                ContentUnavailableView("logs.unavailable", systemImage: "exclamationmark.triangle", description: Text(previewError))
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    private func loadPreview(_ processID: String) async {
        do {
            preview = try await model.logs(for: processID)
            previewError = nil
        } catch {
            previewError = error.localizedDescription
        }
    }

    private var footer: some View {
        HStack {
            Button { openManager(nil) } label: { Label("process.add", systemImage: "plus") }
            Button("manager.open") { openManager(model.config.processes.first?.id) }
            Spacer()
            Button(action: openSettings) { Image(systemName: "gear") }.help("settings.title")
            Button { NSApplication.shared.terminate(nil) } label: { Image(systemName: "power") }
                .help("app.quit")
        }
        .buttonStyle(.borderless)
        .padding(13)
    }

    private var summary: PopoverSummary { PopoverSummary(config: model.config, runtime: model.runtime) }
}

private struct ProcessPopoverRow: View {
    @Environment(\.locale) private var locale
    let process: ProcessDefinition
    let runtime: ProcessRuntime?
    let toggle: () -> Void
    let restart: () -> Void
    let logs: () -> Void
    let edit: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Circle().fill(stateColor).frame(width: 9, height: 9)
            VStack(alignment: .leading, spacing: 2) {
                Text(process.name).font(.subheadline.weight(.semibold)).lineLimit(1)
                Text(statusText).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer()
            Button(action: toggle) { Image(systemName: process.enabled ? "stop.fill" : "play.fill") }
                .help(localized(process.enabled ? "process.disable" : "process.enable"))
            Button(action: restart) { Image(systemName: "arrow.clockwise") }.disabled(!process.enabled).help("process.restart")
            Menu { Button("process.logs", action: logs); Button("process.edit", action: edit) } label: { Image(systemName: "ellipsis.circle") }
                .menuStyle(.borderlessButton)
        }
        .padding(10)
        .background(.quaternary.opacity(0.55), in: RoundedRectangle(cornerRadius: 11))
    }

    private var statusText: String {
        guard process.enabled else { return localized("state.disabled") }
        guard let runtime else { return localized("state.waiting") }
        if runtime.state == .running { return "PID \(runtime.pid ?? 0)" }
        return localized("state.\(runtime.state.rawValue)")
    }
    private func localized(_ key: String) -> String {
        let resource = locale.identifier.hasPrefix("zh") ? "zh-Hans" : "en"
        if let path = Bundle.main.path(forResource: resource, ofType: "lproj"), let bundle = Bundle(path: path) {
            return bundle.localizedString(forKey: key, value: key, table: nil)
        }
        return Bundle.main.localizedString(forKey: key, value: key, table: nil)
    }
    private var stateColor: Color {
        switch runtime?.state {
        case .running: .green
        case .error: .red
        case .waiting, .starting: .orange
        default: .secondary
        }
    }
}
