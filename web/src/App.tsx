import { BrowserRouter as Router, Routes, Route } from 'react-router-dom'
import { AccountProvider } from './context/AccountContext'
import Layout from './components/Layout'
import ChatPage from './pages/ChatPage'
import CoderPage from './pages/CoderPage'
import DashboardPage from './pages/DashboardPage'
import ChannelsPage from './pages/ChannelsPage'
import SessionsPage from './pages/SessionsPage'
import SettingsPage from './pages/SettingsPage'
import AccountsPage from './pages/AccountsPage'

function App() {
  return (
    <AccountProvider>
      <Router>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<ChatPage />} />
            <Route path="code" element={<CoderPage />} />
            <Route path="coder" element={<CoderPage />} />
            <Route path="dashboard" element={<DashboardPage />} />
            <Route path="channels" element={<ChannelsPage />} />
            <Route path="sessions" element={<SessionsPage />} />
            <Route path="accounts" element={<AccountsPage />} />
            <Route path="settings" element={<SettingsPage />} />
          </Route>
        </Routes>
      </Router>
    </AccountProvider>
  )
}

export default App
