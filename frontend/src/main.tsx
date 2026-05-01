import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { ViewerPage } from './pages/ViewerPage.tsx'
import { MobilePage } from './pages/MobilePage.tsx'

const root = createRoot(document.getElementById('root')!)
const path = window.location.pathname

if (path === '/viewer') {
  root.render(<StrictMode><ViewerPage /></StrictMode>)
} else if (path === '/mobile') {
  root.render(<StrictMode><MobilePage /></StrictMode>)
} else {
  root.render(<StrictMode><App /></StrictMode>)
}
