import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  Button,
} from "@qeetrix/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";

type ConfirmOptions = {
  title: string;
  description?: string;
  confirmLabel?: string;
  variant?: "destructive" | "default";
  onConfirm: () => void;
};

export function useConfirmDialog() {
  const { t } = useTranslation();
  const [pending, setPending] = useState<ConfirmOptions | null>(null);

  function openConfirm(opts: ConfirmOptions) {
    setPending(opts);
  }

  const dialog = (
    <AlertDialog
      open={!!pending}
      onOpenChange={(o) => {
        if (!o) setPending(null);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{pending?.title}</AlertDialogTitle>
          {pending?.description && (
            <AlertDialogDescription>{pending.description}</AlertDialogDescription>
          )}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("actions.cancel")}</AlertDialogCancel>
          <Button
            variant={pending?.variant ?? "destructive"}
            onClick={() => {
              pending?.onConfirm();
              setPending(null);
            }}
          >
            {pending?.confirmLabel ?? t("actions.confirm")}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  return [dialog, openConfirm] as const;
}
