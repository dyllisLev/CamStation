import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { ViewerPage } from './pages/ViewerPage.tsx'

const root = createRoot(document.getElementById('root')!)

if (window.location.pathname === '/viewer') {
  root.render(<StrictMode><ViewerPage /></StrictMode>)
} else {
  root.render(<StrictMode><App /></StrictMode>)
}
