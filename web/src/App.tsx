import { BrowserRouter as Router, Routes, Route } from 'react-router-dom'
import Layout from './components/Layout'
import ChatPage from './pages/ChatPage'
import CoderPage from './pages/CoderPage'
import DashboardPage from './pages/DashboardPage'
import ChannelsPage from './pages/ChannelsPage'
import SessionsPage from './pages/SessionsPage'
import SettingsPage from './pages/SettingsPage'

function App() {
  return (
    <Router>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<ChatPage />} />
          <Route path="code" element={<CoderPage />} />
          <Route path="coder" element={<CoderPage />} />
          <Route path="dashboard" element={<DashboardPage />} />
          <Route path="channels" element={<ChannelsPage />} />
          <Route path="sessions" element={<SessionsPage />} />
          <Route path="settings" element={<SettingsPage />} />
        </Route>
      </Routes>
    </Router>
  )
}

export default App
