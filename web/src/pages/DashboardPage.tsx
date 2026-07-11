import { useState, useEffect } from 'react'

interface Stats {
  totalSessions: number
  totalMessages: number
  totalTokens: number
  totalCost: number
  activeChannels: number
}

export default function DashboardPage() {
  const [stats, setStats] = useState<Stats>({
    totalSessions: 0,
    totalMessages: 0,
    totalTokens: 0,
    totalCost: 0,
    activeChannels: 0,
  })

  useEffect(() => {
    // Fetch stats
    fetchStats()
  }, [])

  const fetchStats = async () => {
    try {
      const response = await fetch('/v1/admin/stats')
      if (response.ok) {
        const data = await response.json()
        setStats(data)
      }
    } catch (error) {
      console.error('Failed to fetch stats:', error)
    }
  }

  const statCards = [
    { name: 'Total Sessions', value: stats.totalSessions, icon: '📋' },
    { name: 'Total Messages', value: stats.totalMessages, icon: '💬' },
    { name: 'Total Tokens', value: stats.totalTokens.toLocaleString(), icon: '🔤' },
    { name: 'Total Cost', value: `$${stats.totalCost.toFixed(2)}`, icon: '💰' },
    { name: 'Active Channels', value: stats.activeChannels, icon: '🔗' },
  ]

  return (
    <div className="p-6">
      <h2 className="text-2xl font-bold text-white mb-6">Dashboard</h2>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5 gap-4 mb-8">
        {statCards.map((stat) => (
          <div
            key={stat.name}
            className="bg-gray-800 rounded-lg p-4 border border-gray-700"
          >
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">{stat.name}</p>
                <p className="text-2xl font-bold text-white mt-1">{stat.value}</p>
              </div>
              <span className="text-3xl">{stat.icon}</span>
            </div>
          </div>
        ))}
      </div>

      {/* Charts placeholder */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
          <h3 className="text-lg font-semibold text-white mb-4">Usage Over Time</h3>
          <div className="h-64 flex items-center justify-center text-gray-500">
            Chart placeholder
          </div>
        </div>

        <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
          <h3 className="text-lg font-semibold text-white mb-4">Model Distribution</h3>
          <div className="h-64 flex items-center justify-center text-gray-500">
            Chart placeholder
          </div>
        </div>
      </div>
    </div>
  )
}
