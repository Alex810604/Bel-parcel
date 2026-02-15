import { Container, AppBar, Toolbar, Typography, Button } from '@mui/material'
import { Outlet, Link } from 'react-router-dom'
import { useEffect, useState } from 'react'

export default function App() {
  const [alertCount, setAlertCount] = useState<number>(() => {
    const v = parseInt(localStorage.getItem('alert_count') || '0', 10)
    return isNaN(v) ? 0 : v
  })
  const [soundEnabled, setSoundEnabled] = useState<boolean>(() => {
    return (localStorage.getItem('sound_enabled') ?? 'true') !== 'false'
  })
  useEffect(() => {
    function onNew(e: any) {
      const c = e?.detail?.count
      if (typeof c === 'number') setAlertCount(c)
      else {
        const v = parseInt(localStorage.getItem('alert_count') || '0', 10)
        setAlertCount(isNaN(v) ? 0 : v)
      }
    }
    window.addEventListener('alert:new', onNew as EventListener)
    return () => window.removeEventListener('alert:new', onNew as EventListener)
  }, [])
  function resetAlerts() {
    localStorage.setItem('alert_count', '0')
    setAlertCount(0)
  }
  function toggleSound() {
    const next = !soundEnabled
    setSoundEnabled(next)
    localStorage.setItem('sound_enabled', next ? 'true' : 'false')
  }
  return (
    <>
      <AppBar position="static">
        <Toolbar>
          <Typography variant="h6" component="div" sx={{ flexGrow: 1 }}>
            BelParcel Operator Panel
          </Typography>
          <Link to="/" style={{ color: '#fff', textDecoration: 'none', marginRight: 16 }}>Рейсы</Link>
          <Link to="/map" style={{ color: '#fff', textDecoration: 'none' }}>Карта</Link>
          <Typography sx={{ ml: 2, mr: 1, color: alertCount > 0 ? 'error.main' : '#fff' }}>
            Алерты: {alertCount}
          </Typography>
          <Button color="inherit" onClick={resetAlerts} sx={{ mr: 1 }}>Сбросить</Button>
          <Button color="inherit" onClick={toggleSound}>Звук: {soundEnabled ? 'вкл' : 'выкл'}</Button>
        </Toolbar>
      </AppBar>
      <Container sx={{ mt: 2 }}>
        <Outlet />
      </Container>
    </>
  )
}
