import { useEffect } from 'react'
import { App, Form, Select, type FormInstance } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, put, errorMessage } from '../../api/client'
import type { AllSettings } from '../../api/types'

export interface SettingsResponse { settings: AllSettings; roles: string[]; public_base: string; keycloak_redirect_uri: string; keycloak_post_logout_uri: string }

export function useSettings() {
  return useQuery({ queryKey: ['admin', 'settings'], queryFn: () => get<SettingsResponse>('/admin/settings') })
}

/** Binds a settings section to a form: loads values once, saves partial updates. */
export function useSectionForm<K extends keyof AllSettings>(section: K, form: FormInstance) {
  const { data, isLoading } = useSettings()
  const qc = useQueryClient()
  const { message } = App.useApp()
  const values = data?.settings[section]
  useEffect(() => {
    if (values) form.setFieldsValue(values as never)
  }, [values, form])
  const save = useMutation({
    mutationFn: (v: Partial<AllSettings[K]>) => put<{ settings: AllSettings }>(`/admin/settings/${section}`, v),
    onSuccess: (r) => {
      qc.setQueryData(['admin', 'settings'], (old: SettingsResponse | undefined) => (old ? { ...old, settings: r.settings } : old))
      form.setFieldsValue(r.settings[section] as never)
      void qc.invalidateQueries({ queryKey: ['admin', 'dashboard'] })
      message.success('저장했습니다')
    },
    onError: (e) => message.error(errorMessage(e), 6),
  })
  return { values, roles: data?.roles ?? [], isLoading, save }
}

/** Free-form string list input (CIDRs, origins, roles...). */
export function TagsInput({ placeholder, options, ...rest }: { placeholder?: string; options?: string[]; value?: string[]; onChange?: (v: string[]) => void; id?: string }) {
  return (
    <Select
      mode="tags"
      tokenSeparators={[',', ' ', '\n']}
      placeholder={placeholder}
      options={options?.map((o) => ({ value: o, label: o }))}
      open={options ? undefined : false}
      suffixIcon={null}
      {...rest}
    />
  )
}

export const useStableForm = () => Form.useForm()[0]
