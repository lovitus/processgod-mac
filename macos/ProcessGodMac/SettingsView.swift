import ServiceManagement
import SwiftUI

struct SettingsView: View {
    @Bindable var model: AppModel
    @State private var pathEnv = ""
    @State private var editingPath = false

    var body: some View {
        Form {
            Section("settings.startup") {
                Picker("settings.level", selection: Binding(get: { model.serviceSelection }, set: { selection in Task { await model.switchService(to: selection) } })) {
                    Text("service.user").tag(ServiceSelection.user)
                    Text("service.system").tag(ServiceSelection.system)
                }.pickerStyle(.radioGroup)
                if model.serviceRequiresApproval {
                    Label("service.approvalRequired", systemImage: "exclamationmark.shield").foregroundStyle(.orange)
                    Button("service.openSettings") { model.services.openApprovalSettings() }
                }
            }
            Section("settings.path") {
                TextField("PATH", text: $pathEnv)
                    .font(.system(.body, design: .monospaced))
                    .disabled(!editingPath)
                HStack {
                    if editingPath {
                        Button("common.save") {
                            Task {
                                await model.updatePath(pathEnv)
                                if model.connectionError == nil { editingPath = false }
                            }
                        }
                        .buttonStyle(.borderedProminent)
                        Button("common.cancel") {
                            pathEnv = model.config.pathEnv
                            editingPath = false
                        }
                    } else {
                        Button("common.modify") { editingPath = true }
                    }
                }
            }
            Section("settings.language") {
                Picker("settings.language", selection: $model.languageOverride) {
                    Text("language.system").tag("system")
                    Text("English").tag("en")
                    Text("简体中文").tag("zh-Hans")
                }
            }
        }
        .formStyle(.grouped)
        .frame(width: 540, height: 420)
        .padding()
        .onAppear { pathEnv = model.config.pathEnv }
        .environment(\.locale, model.locale)
        .safeAreaInset(edge: .top) {
            if let error = model.connectionError {
                Text(error).font(.caption).foregroundStyle(.red).frame(maxWidth: .infinity, alignment: .leading).padding(8).background(.red.opacity(0.08))
            }
        }
    }
}
