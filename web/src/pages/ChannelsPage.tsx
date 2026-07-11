import { useState, useEffect } from 'react'

interface Channel {
  id: number
  name: string
  type: number
  base_url: string
  models: string
  status: number
  weight: number
  priority: number
}

export default function ChannelsPage() {
  const [channels, setChannels] = useState<Channel[]>([])
  const [showAdd, setShowAdd] = useState(false)

  useEffect(() => {
    fetchChannels()
  }, [])

  const fetchChannels = async () => {
    try {
      const response = await fetch('/v1/admin/channels')
      if (response.ok) {
        const data = await response.json()
        setChannels(data.channels || [])
      }
    } catch (error) {
      console.error('Failed to fetch channels:', error)
    }
  }

  const channelTypes: Record<number, string> = {
    1: 'OpenAI',
    2: 'Claude',
    3: 'Gemini',
    4: 'DeepSeek',
    5: 'Ollama',
    99: 'Custom',
  }

  return (
    <div className="p-6">
      <div className="flex justify-between items-center mb-6">
        <h2 className="text-2xl font-bold text-white">Channels</h2>
        <button
          onClick={() => setShowAdd(true)}
          className="bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700"
        >
          Add Channel
        </button>
      </div>

      {/* Channels Table */}
      <div className="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
        <table className="w-full">
          <thead>
            <tr className="border-b border-gray-700">
              <th className="px-4 py-3 text-left text-sm font-medium text-gray-400">ID</th>
              <th className="px-4 py-3 text-left text-sm font-medium text-gray-400">Name</th>
              <th className="px-4 py-3 text-left text-sm font-medium text-gray-400">Type</th>
              <th className="px-4 py-3 text-left text-sm font-medium text-gray-400">Base URL</th>
              <th className="px-4 py-3 text-left text-sm font-medium text-gray-400">Status</th>
              <th className="px-4 py-3 text-left text-sm font-medium text-gray-400">Weight</th>
              <th className="px-4 py-3 text-left text-sm font-medium text-gray-400">Actions</th>
            </tr>
          </thead>
          <tbody>
            {channels.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-gray-500">
                  No channels configured
                </td>
              </tr>
            ) : (
              channels.map((channel) => (
                <tr key={channel.id} className="border-b border-gray-700 hover:bg-gray-750">
                  <td className="px-4 py-3 text-sm text-gray-300">{channel.id}</td>
                  <td className="px-4 py-3 text-sm text-white font-medium">{channel.name}</td>
                  <td className="px-4 py-3 text-sm text-gray-300">
                    {channelTypes[channel.type] || 'Unknown'}
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-400">{channel.base_url}</td>
                  <td className="px-4 py-3">
                    <span className={`px-2 py-1 text-xs rounded-full ${
                      channel.status === 1 
                        ? 'bg-green-900 text-green-300' 
                        : 'bg-red-900 text-red-300'
                    }`}>
                      {channel.status === 1 ? 'Active' : 'Inactive'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-300">{channel.weight}</td>
                  <td className="px-4 py-3">
                    <button className="text-blue-400 hover:text-blue-300 text-sm">
                      Edit
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
