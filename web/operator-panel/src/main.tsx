import React from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import App from './App'
import Trips from './pages/Trips'
import TripDetails from './pages/TripDetails'
import MapPage from './pages/Map'

const root = createRoot(document.getElementById('root')!)
root.render(
  <BrowserRouter>
    <Routes>
      <Route path="/" element={<App />}>
        <Route index element={<Trips />} />
        <Route path="/trips/:tripId" element={<TripDetails />} />
        <Route path="/map" element={<MapPage />} />
      </Route>
    </Routes>
  </BrowserRouter>
)
