import SwiftUI

struct ManagerView: View {
    @Bindable var model: AppModel
    @State private var selection: String?
    @State private var creating = false
    @State private var pendingDelete: String?
    let initialSelection: String?
    let openLogs: (String) -> Void

    init(model: AppModel, initialSelection: String?, openLogs: @escaping (String) -> Void) {
        self.model = model
        self.initialSelection = initialSelection
        self.openLogs = openLogs
        _selection = State(initialValue: initialSelection)
        _creating = State(initialValue: initialSelection == nil)
    }

    var body: some View {
        NavigationSplitView {
            List(selection: $selection) {
                ForEach(model.config.processes) { process in
                    HStack {
                        Circle().fill(color(for: process.id)).frame(width: 8, height: 8)
                        VStack(alignment: .leading) {
                            Text(process.name)
                            Text(process.command).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                    .tag(process.id)
                }
            }
            .navigationTitle("processes.title")
            .toolbar {
                Button { creating = true; selection = nil } label: { Image(systemName: "plus") }
                Button { pendingDelete = selection } label: { Image(systemName: "trash") }
                    .disabled(selection == nil)
            }
        } detail: {
            if creating {
                ProcessEditor(model: model, process: .empty, originalID: nil) { creating = false; selection = $0 }
            } else if let selection, let process = model.config.processes.first(where: { $0.id == selection }) {
                ProcessEditor(model: model, process: process, originalID: selection) { self.selection = $0 }
                    .toolbar { Button("process.logs") { openLogs(selection) } }
            } else {
                ContentUnavailableView("process.select", systemImage: "sidebar.left")
            }
        }
        .frame(minWidth: 980, minHeight: 640)
        .environment(\.locale, model.locale)
        .safeAreaInset(edge: .top) {
            if let error = model.connectionError {
                Text(error).font(.caption).foregroundStyle(.red).frame(maxWidth: .infinity, alignment: .leading).padding(8).background(.red.opacity(0.08))
            }
        }
        .confirmationDialog("process.delete.confirm", isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } })) {
            Button("process.delete", role: .destructive) {
                guard let id = pendingDelete else { return }
                Task {
                    if await model.delete(id) { selection = nil }
                    pendingDelete = nil
                }
            }
            Button("common.cancel", role: .cancel) { pendingDelete = nil }
        }
    }

    private func color(for id: String) -> Color {
        switch model.runtime.processes.first(where: { $0.id == id })?.state {
        case .running: .green
        case .error: .red
        case .waiting, .starting: .orange
        default: .secondary
        }
    }
}
