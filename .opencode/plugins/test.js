// Minimal plugin to test loading
export const TestPlugin = async () => {
  try {
    // Write to log file to confirm plugin loaded
    await Bun.write(".pmai/logs/plugin-test.log", "TestPlugin loaded at " + new Date().toISOString() + "\n")
  } catch (_) {}
  return {}
}
