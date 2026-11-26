import { useParams } from "@tanstack/react-router";
import { useCallback, useState } from "react";
import { $api, fetchClient } from "@/client";
import { Failed, Meta, PageHead } from "@/components/app";
import { type Place, SignForm } from "@/components/app/signing";
import { useSession } from "@/session";

// SignPage is a member signing their own place on an agreement, signed in.
export function SignPage() {
  const { org } = useSession();
  const { id } = useParams({ from: "/o/$org/agreements/$id/sign" });
  const path = { params: { path: { org, id } } };
  const agreement = $api.useQuery("get", "/o/{org}/api/v1/agreements/{id}", path);
  const fields = $api.useQuery("get", "/o/{org}/api/v1/agreements/{id}/fields", { params: { path: { org, id }, query: { limit: 200 } } });
  const recipients = $api.useQuery("get", "/o/{org}/api/v1/agreements/{id}/recipients", { params: { path: { org, id }, query: { limit: 200 } } });
  const sign = $api.useMutation("post", "/o/{org}/api/v1/agreements/{id}/sign", { onSuccess: () => agreement.refetch() });
  const decline = $api.useMutation("post", "/o/{org}/api/v1/agreements/{id}/decline", { onSuccess: () => agreement.refetch() });
  const [done, setDone] = useState("");
  const load = useCallback(async () => {
    const r = await fetchClient.GET("/o/{org}/api/v1/agreements/{id}/document", { params: { path: { org, id } }, parseAs: "arrayBuffer" });
    if (r.error || !r.data) throw new Error("The document couldn't be fetched.");
    return r.data;
  }, [org, id]);

  const a = agreement.data;
  if (agreement.error) return <Failed errors={[agreement.error]} />;
  if (!a || !fields.data || !recipients.data) return <Meta>Reading the agreement…</Meta>;
  const me = recipients.data.items.find((r) => r.id === a.my_recipient);
  const places: Place[] = fields.data.items.filter((f) => f.recipient_id === a.my_recipient).map(({ id, page, kind, label, x, y, w, h }) => ({ id, page, kind, label: label ?? "", x, y, w, h }));

  if (done || !a.my_turn) {
    return (
      <>
        <PageHead title={done || (me?.status === "signed" ? "You've signed" : "Nothing to sign here")} />
        <Meta>
          {a.name} · {a.status}
        </Meta>
      </>
    );
  }
  return (
    <>
      <PageHead title="Sign the agreement">
        <Meta>
          {a.name} · for {a.company}
        </Meta>
      </PageHead>
      <div className="flex max-w-3xl flex-col gap-4">
        <SignForm
          places={places}
          load={load}
          sentName={me?.name ?? ""}
          consent={a.consent}
          busy={sign.isPending || decline.isPending}
          error={(sign.error ?? decline.error)?.detail ?? ""}
          onSign={(x) => sign.mutate({ ...path, body: { typed_name: x.typed, consent: true, values: x.values } }, { onSuccess: () => setDone("You've signed") })}
          onDecline={(reason) => decline.mutate({ ...path, body: { reason } }, { onSuccess: () => setDone("You declined it") })}
        />
      </div>
    </>
  );
}
