import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'
import { userFacingError } from '../api/httpClient'

const schema = z.object({
  username: z.string().trim().min(1, '請輸入帳號'),
  password: z.string().min(1, '請輸入密碼'),
})

export default function LoginPage() {
  const { register, handleSubmit, formState: { isSubmitting, errors } } = useForm({ resolver: zodResolver(schema) })
  const { login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [submitError, setSubmitError] = useState('')

  const submit = async (data) => {
    setSubmitError('')
    try {
      await login(data)
      navigate(location.state?.from?.pathname || '/admin', { replace: true })
    } catch (error) {
      setSubmitError(error?.response?.status === 429 ? userFacingError(error) : '帳號或密碼錯誤，或目前無法登入，請稍後再試。')
    }
  }

  return <main className="login-page">
    <form className="login-card" onSubmit={handleSubmit(submit)} noValidate>
      <div><span className="brand-mark">A</span><h1>點數媒合管理後台</h1><p>請使用授權帳號登入。</p></div>
      <label>帳號<input autoComplete="username" {...register('username')} /></label>
      {errors.username && <p className="field-error" role="alert">{errors.username.message}</p>}
      <label>密碼<input autoComplete="current-password" type="password" {...register('password')} /></label>
      {errors.password && <p className="field-error" role="alert">{errors.password.message}</p>}
      {submitError && <p className="field-error" role="alert">{submitError}</p>}
      <button className="search-button" disabled={isSubmitting}>{isSubmitting ? '登入中…' : '登入'}</button>
    </form>
  </main>
}
