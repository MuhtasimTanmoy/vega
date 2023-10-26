-- +goose Up
alter table referral_set_stats
    add column rewards_multiplier text not null default '0';

alter table referral_set_stats
    add column rewards_factor_multiplier text not null default '0';

alter table referral_set_stats
    add column was_eligible bool not null default true;

-- +goose Down

alter table referral_set_stats
    drop column rewards_multiplier;

alter table referral_set_stats
    drop column rewards_factor_multiplier;

alter table referral_set_stats
    drop column was_eligible;
