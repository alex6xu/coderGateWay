export default function SettingsPage() {
  return (
    <div className="p-6">
      <h2 className="text-2xl font-bold text-white mb-6">Settings</h2>

      <div className="space-y-6">
        {/* General Settings */}
        <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
          <h3 className="text-lg font-semibold text-white mb-4">General</h3>
          <div className="space-y-4">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Default Model</label>
              <select className="w-full bg-gray-700 text-white rounded-lg px-4 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500">
                <option value="gpt-4o">GPT-4o</option>
                <option value="claude-3-5-sonnet">Claude 3.5 Sonnet</option>
                <option value="deepseek-v3">DeepSeek V3</option>
              </select>
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">Temperature</label>
              <input
                type="range"
                min="0"
                max="2"
                step="0.1"
                defaultValue="0.7"
                className="w-full"
              />
            </div>
          </div>
        </div>

        {/* Gateway Settings */}
        <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
          <h3 className="text-lg font-semibold text-white mb-4">Gateway</h3>
          <div className="space-y-4">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Routing Strategy</label>
              <select className="w-full bg-gray-700 text-white rounded-lg px-4 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500">
                <option value="auto">Auto</option>
                <option value="cost">Cost</option>
                <option value="latency">Latency</option>
                <option value="quality">Quality</option>
              </select>
            </div>
            <div className="flex items-center">
              <input
                type="checkbox"
                id="fallback"
                defaultChecked
                className="mr-2"
              />
              <label htmlFor="fallback" className="text-sm text-gray-400">
                Enable fallback
              </label>
            </div>
          </div>
        </div>

        {/* Platform Settings */}
        <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
          <h3 className="text-lg font-semibold text-white mb-4">Platforms</h3>
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-white">Web</p>
                <p className="text-sm text-gray-500">WebSocket interface</p>
              </div>
              <span className="px-2 py-1 text-xs bg-green-900 text-green-300 rounded">
                Active
              </span>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="text-white">Telegram</p>
                <p className="text-sm text-gray-500">Bot integration</p>
              </div>
              <span className="px-2 py-1 text-xs bg-gray-700 text-gray-400 rounded">
                Inactive
              </span>
            </div>
          </div>
        </div>

        {/* Save Button */}
        <div className="flex justify-end">
          <button className="bg-blue-600 text-white px-6 py-2 rounded-lg hover:bg-blue-700">
            Save Settings
          </button>
        </div>
      </div>
    </div>
  )
}
