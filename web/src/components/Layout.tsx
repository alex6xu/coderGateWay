import { Outlet, Link, useLocation } from 'react-router-dom'

const navigation = [
  { name: 'Chat', href: '/', icon: '💬' },
  { name: 'Dashboard', href: '/dashboard', icon: '📊' },
  { name: 'Channels', href: '/channels', icon: '🔗' },
  { name: 'Sessions', href: '/sessions', icon: '📋' },
  { name: 'Settings', href: '/settings', icon: '⚙️' },
]

export default function Layout() {
  const location = useLocation()

  return (
    <div className="flex h-screen">
      {/* Sidebar */}
      <aside className="w-64 bg-gray-900 border-r border-gray-800">
        <div className="p-4">
          <h1 className="text-xl font-bold text-blue-400">CodeGateway</h1>
          <p className="text-sm text-gray-500">AI Agent + API Gateway</p>
        </div>
        
        <nav className="mt-4">
          {navigation.map((item) => {
            const isActive = location.pathname === item.href
            return (
              <Link
                key={item.name}
                to={item.href}
                className={`flex items-center px-4 py-3 text-sm ${
                  isActive
                    ? 'bg-blue-600 text-white'
                    : 'text-gray-400 hover:bg-gray-800 hover:text-white'
                }`}
              >
                <span className="mr-3">{item.icon}</span>
                {item.name}
              </Link>
            )
          })}
        </nav>

        <div className="absolute bottom-0 w-64 p-4 border-t border-gray-800">
          <div className="flex items-center">
            <div className="w-8 h-8 bg-blue-600 rounded-full flex items-center justify-center">
              <span className="text-white text-sm font-medium">A</span>
            </div>
            <div className="ml-3">
              <p className="text-sm font-medium text-white">Admin</p>
              <p className="text-xs text-gray-500">admin@codegateway.local</p>
            </div>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  )
}
