import { createRoot } from 'react-dom/client'
import './styles/base.css'
import './styles/components.css'
import './styles/app.css'
import { App } from './App'

// The desktop window opens /?token=…: drop the launch token from the address.
if (location.search.includes('token=')) history.replaceState(null, '', '/')

createRoot(document.getElementById('root')!).render(<App />)
