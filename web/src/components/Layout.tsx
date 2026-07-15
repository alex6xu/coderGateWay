import { Outlet, Link, useLocation } from 'react-router-dom'

const navigation = [
  { name: 'Chat', href: '/', icon: '💬' },
  { name: 'Code', href: '/code', icon: '🛠️' },
  { name: 'Dashboard', href: '/dashboard', icon: '📊' },
  { name: 'Channels', href: '/channels', icon: '🔗' },
  { name: 'Sessions', href: '/sessions', icon: '📋' },
  { name: 'Settings', href: '/settings', icon: '⚙️' },
]

export default function Layout() {
  const location = useLocation()

  return (
    <div className="flex h-screen bg-background">
      {/* Sidebar */}
      <aside className="w-60 border-r border-border flex flex-col bg-card">
        {/* Logo */}
        <div className="h-14 flex items-center px-5 border-b border-border">
          <div className="flex items-center gap-2.5">
            <div className="w-7 h-7 rounded-lg bg-primary flex items-center justify-center">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <polyline points="16 18 22 12 16 6" />
                <polyline points="8 6 2 12 8 18" />
              </svg>
            </div>
            <div>
              <h1 className="text-sm font-semibold text-foreground">CodeGateway</h1>
              <p className="text-[10px] text-muted-foreground">AI Agent + API Gateway</p>
            </div>
          </div>
        </div>

        {/* Navigation */}
        <nav className="flex-1 px-3 py-3 space-y-0.5">
          {navigation.map((item) => {
            const isActive =
              item.href === '/'
                ? location.pathname === '/'
                : location.pathname === item.href || location.pathname.startsWith(item.href + '/')
            // Also highlight Code for legacy /coder path
            const codeActive = item.href === '/code' && (location.pathname === '/coder' || location.pathname.startsWith('/coder/'))
            const active = isActive || codeActive
            return (
              <Link
                key={item.name}
                to={item.href}
                className={`flex items-center gap-2.5 px-3 py-2 rounded-md text-[13px] font-medium transition-colors ${
                  active
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                }`}
              >
                <span className="text-base">{item.icon}</span>
                {item.name}
              </Link>
            )
          })}
        </nav>

        {/* User */}
        <div className="p-3 border-t border-border">
          <div className="flex items-center gap-2.5 px-3 py-2 rounded-md hover:bg-accent cursor-pointer transition-colors">
            <div className="w-7 h-7 rounded-full bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center">
              <span className="text-white text-xs font-medium">A</span>
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-[13px] font-medium text-foreground truncate">Admin</p>
              <p className="text-[11px] text-muted-foreground truncate">admin@codegateway.local</p>
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
