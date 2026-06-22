import SwiftUI

struct LogsView: View {
    @Bindable var model: AppModel
    let processID: String
    @State private var snapshot: LogSnapshot?
    @State private var selection = 0
    @State private var error: String?

    var body: some View {
        VStack(spacing: 0) {
            Picker("logs.category", selection: $selection) {
                Text("logs.errorWarning").tag(0)
                Text("logs.standardOther").tag(1)
            }.pickerStyle(.segmented).padding()
            if let snapshot {
                let buffer = selection == 0 ? snapshot.errorWarning : snapshot.standardOther
                HStack {
                    Text("\(buffer.kept)/\(buffer.capacity)").monospacedDigit()
                    Text("\(model.localized("logs.totalSeen")): \(snapshot.totalSeen)").monospacedDigit()
                    Text("\(model.localized("logs.lineLimit")): \(snapshot.lineMaxBytes) B").monospacedDigit()
                    Spacer()
                    Text("logs.memoryOnly").foregroundStyle(.secondary)
                }
                .font(.caption)
                .padding(.horizontal)
                List(buffer.entries) { entry in
                    VStack(alignment: .leading, spacing: 3) {
                        HStack { Text("#\(entry.sequence)").monospacedDigit(); Text(entry.source); Spacer(); Text(entry.timestamp, style: .time) }.font(.caption).foregroundStyle(.secondary)
                        Text(entry.text).font(.system(.body, design: .monospaced)).textSelection(.enabled)
                    }.padding(.vertical, 3)
                }
            } else if let error { ContentUnavailableView("logs.unavailable", systemImage: "exclamationmark.triangle", description: Text(error)) }
            else { ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity) }
        }
        .frame(minWidth: 760, minHeight: 520)
        .navigationTitle(processID)
        .task(id: model.logEventGeneration) {
            try? await Task.sleep(for: .milliseconds(snapshot == nil ? 0 : 150))
            guard !Task.isCancelled else { return }
            await load()
        }
        .environment(\.locale, model.locale)
    }

    private func load() async {
        do { snapshot = try await model.logs(for: processID) }
        catch { self.error = error.localizedDescription }
    }
}
