export default function SettingsPage() {
  return (
    <div className="p-6">
      <div className="mb-6">
        <h2 className="text-base font-semibold text-foreground">Settings</h2>
        <p className="text-[13px] text-muted-foreground mt-0.5">Configure your CodeGateway instance</p>
      </div>

      <div className="space-y-4 max-w-2xl">
        {/* General Settings */}
        <div className="bg-card border border-border rounded-xl p-5">
          <h3 className="text-sm font-semibold text-foreground mb-4">General</h3>
          <div className="space-y-4">
            <div>
              <label className="block text-[13px] font-medium text-foreground mb-1.5">Default Model</label>
              <select className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent transition-colors">
                <option value="gpt-4o">GPT-4o</option>
                <option value="claude-3-5-sonnet">Claude 3.5 Sonnet</option>
                <option value="deepseek-v3">DeepSeek V3</option>
              </select>
            </div>
            <div>
              <label className="block text-[13px] font-medium text-foreground mb-1.5">Temperature</label>
              <div className="flex items-center gap-3">
                <input
                  type="range"
                  min="0"
                  max="2"
                  step="0.1"
                  defaultValue="0.7"
                  className="flex-1 accent-primary"
                />
                <span className="text-[13px] text-muted-foreground tabular-nums w-8 text-right">0.7</span>
              </div>
            </div>
            <div>
              <label className="block text-[13px] font-medium text-foreground mb-1.5">Max Tokens</label>
              <input
                type="number"
                defaultValue={128000}
                className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent transition-colors"
              />
            </div>
          </div>
        </div>

        {/* Gateway Settings */}
        <div className="bg-card border border-border rounded-xl p-5">
          <h3 className="text-sm font-semibold text-foreground mb-4">Gateway</h3>
          <div className="space-y-4">
            <div>
              <label className="block text-[13px] font-medium text-foreground mb-1.5">Routing Strategy</label>
              <select className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent transition-colors">
                <option value="auto">Auto</option>
                <option value="cost">Cost</option>
                <option value="latency">Latency</option>
                <option value="quality">Quality</option>
              </select>
            </div>
            <div className="flex items-center gap-3">
              <input
                type="checkbox"
                id="fallback"
                defaultChecked
                className="w-4 h-4 rounded border-border text-primary focus:ring-ring"
              />
              <label htmlFor="fallback" className="text-[13px] text-foreground cursor-pointer">
                Enable fallback
              </label>
            </div>
            <div className="flex items-center gap-3">
              <input
                type="checkbox"
                id="retry"
                defaultChecked
                className="w-4 h-4 rounded border-border text-primary focus:ring-ring"
              />
              <label htmlFor="retry" className="text-[13px] text-foreground cursor-pointer">
                Enable retry on failure
              </label>
            </div>
          </div>
        </div>

        {/* Platform Settings */}
        <div className="bg-card border border-border rounded-xl p-5">
          <h3 className="text-sm font-semibold text-foreground mb-4">Platforms</h3>
          <div className="space-y-3">
            <div className="flex items-center justify-between py-2">
              <div className="flex items-center gap-3">
                <span className="text-lg">🌐</span>
                <div>
                  <p className="text-[13px] font-medium text-foreground">Web</p>
                  <p className="text-[12px] text-muted-foreground">WebSocket interface</p>
                </div>
              </div>
              <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-success">
                <span className="w-1.5 h-1.5 rounded-full bg-success"></span>
                Active
              </span>
            </div>
            <div className="border-t border-border"></div>
            <div className="flex items-center justify-between py-2">
              <div className="flex items-center gap-3">
                <span className="text-lg">📱</span>
                <div>
                  <p className="text-[13px] font-medium text-foreground">Telegram</p>
                  <p className="text-[12px] text-muted-foreground">Bot integration</p>
                </div>
              </div>
              <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-muted-foreground">
                <span className="w-1.5 h-1.5 rounded-full bg-muted-foreground"></span>
                Inactive
              </span>
            </div>
            <div className="border-t border-border"></div>
            <div className="flex items-center justify-between py-2">
              <div className="flex items-center gap-3">
                <span className="text-lg">💻</span>
                <div>
                  <p className="text-[13px] font-medium text-foreground">Terminal</p>
                  <p className="text-[12px] text-muted-foreground">CLI interface</p>
                </div>
              </div>
              <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-success">
                <span className="w-1.5 h-1.5 rounded-full bg-success"></span>
                Active
              </span>
            </div>
          </div>
        </div>

        {/* Save Button */}
        <div className="flex justify-end pt-2">
          <button className="h-9 px-5 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 transition-colors">
            Save Settings
          </button>
        </div>
      </div>
    </div>
  )
}
