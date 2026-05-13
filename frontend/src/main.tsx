import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { ViewerPage } from './pages/ViewerPage.tsx'
import { MobilePage } from './pages/MobilePage.tsx'
import { NewCamStation } from './pages/new-ui/NewCamStation.tsx'
import { getAppEntryForPath } from './routes.ts'

const root = createRoot(document.getElementById('root')!)
const entry = getAppEntryForPath(window.location.pathname)

if (entry === 'viewer') {
  root.render(<StrictMode><ViewerPage /></StrictMode>)
} else if (entry === 'mobile') {
  root.render(<StrictMode><MobilePage /></StrictMode>)
} else if (entry === 'new') {
  root.render(<StrictMode><NewCamStation /></StrictMode>)
} else {
  root.render(<StrictMode><App /></StrictMode>)
}
