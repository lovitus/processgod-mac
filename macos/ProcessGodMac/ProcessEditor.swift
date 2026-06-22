import AppKit
import SwiftUI

private struct EnvironmentPair: Identifiable {
    let id = UUID()
    var key: String
    var value: String
}

struct ProcessEditor: View {
    @Bindable var model: AppModel
    let originalID: String?
    let saved: (String) -> Void
    @State private var draft: ProcessDefinition
    @State private var environment: [EnvironmentPair]
    @State private var cronValid = true

    init(model: AppModel, process: ProcessDefinition, originalID: String?, saved: @escaping (String) -> Void) {
        self.model = model; self.originalID = originalID; self.saved = saved
        _draft = State(initialValue: process)
        _environment = State(initialValue: process.environment.sorted { $0.key < $1.key }.map { EnvironmentPair(key: $0.key, value: $0.value) })
    }

    var body: some View {
        Form {
            Section("editor.basic") {
                TextField("editor.name", text: $draft.name)
                HStack {
                    TextField("editor.command", text: $draft.command)
                    Button("editor.choose") { chooseExecutable() }
                }
                TextField("editor.arguments", text: $draft.arguments)
                HStack {
                    TextField("editor.workingDirectory", text: $draft.workingDirectory)
                    Button("editor.choose") { chooseDirectory() }
                }
            }
            Section("editor.behavior") {
                Picker("editor.mode", selection: $draft.mode) {
                    ForEach(ProcessMode.allCases) { mode in Text(LocalizedStringKey(mode.titleKey)).tag(mode) }
                }
                Toggle("editor.enabled", isOn: $draft.enabled)
                if draft.mode.usesCron {
                    TextField("editor.cron", text: $draft.cronExpression)
                        .onChange(of: draft.cronExpression) { _, value in Task { cronValid = await model.validateCron(value) } }
                    if !cronValid { Text("editor.cron.invalid").foregroundStyle(.red).font(.caption) }
                }
            }
            Section("editor.environment") {
                ForEach($environment) { $pair in
                    HStack { TextField("KEY", text: $pair.key); TextField("VALUE", text: $pair.value); Button { environment.removeAll { $0.id == pair.id } } label: { Image(systemName: "minus.circle") } }
                }
                Button("editor.environment.add") { environment.append(.init(key: "", value: "")) }
            }
            Section("editor.advanced") {
                TextField("editor.id", text: $draft.id)
                    .disabled(originalID != nil)
            }
            HStack {
                Spacer()
                Button("common.save") { save() }.buttonStyle(.borderedProminent).disabled(!canSave || model.isBusy)
            }
        }
        .formStyle(.grouped)
        .navigationTitle(originalID == nil ? "process.add" : "process.edit")
        .padding()
    }

    private var canSave: Bool { ProcessEditorValidation.canSave(draft, cronValid: cronValid) }
    private func save() {
        var values: [String: String] = [:]
        for pair in environment where !pair.key.isEmpty { values[pair.key] = pair.value }
        draft.environment = values
        if draft.id.isEmpty { draft.id = draft.name.lowercased().replacingOccurrences(of: " ", with: "-") }
        let id = draft.id
        Task {
            if await model.save(draft, replacing: originalID) { saved(id) }
        }
    }
    private func chooseExecutable() {
        let panel = NSOpenPanel(); panel.canChooseDirectories = false; panel.allowsMultipleSelection = false
        if panel.runModal() == .OK, let url = panel.url { draft.command = url.path; if draft.workingDirectory.isEmpty { draft.workingDirectory = url.deletingLastPathComponent().path } }
    }
    private func chooseDirectory() {
        let panel = NSOpenPanel(); panel.canChooseFiles = false; panel.canChooseDirectories = true
        if panel.runModal() == .OK, let url = panel.url { draft.workingDirectory = url.path }
    }
}
