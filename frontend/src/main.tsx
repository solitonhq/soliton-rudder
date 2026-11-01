import React from 'react'
import { createRoot } from 'react-dom/client'
import './themes/themes.css'
import App from './App'
import { installPlainInputs } from './lib/plainInputs'

installPlainInputs()

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
